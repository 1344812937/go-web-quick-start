<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown, Clock, Coin, Connection, CopyDocument, DataLine, Odometer, Refresh, Tickets, Timer } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import RequestTrendChart from '@/pages/home/RequestTrendChart.vue'
import type { Channel, ClientToken, DashboardSummary, GatewayModel } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatCompactNumber, formatDuration } from '@/utils/formatters'

const router = useRouter()
const loading = ref(true)
const errorMessage = ref('')
const dashboard = ref<DashboardSummary | null>(null)
const channels = ref<Channel[]>([])
const models = ref<GatewayModel[]>([])
const tokens = ref<ClientToken[]>([])
const defaultCodexModel = 'gpt-5.6-sol'
const selectedModel = ref('')
const serviceBaseUrl = ref(typeof window === 'undefined' ? '/v1' : `${window.location.origin}/v1`)
const copyingConfig = ref(false)
const quickStartExpanded = ref(false)

type DashboardRangeDays = 1 | 2 | 3 | 5
type TrendDimension = 'hour' | 'day'

const selectedRangeDays = ref<DashboardRangeDays>(1)
const trendDimension = ref<TrendDimension>('hour')
const timeRangeOptions: Array<{ label: string; value: DashboardRangeDays }> = [
  { label: '当前', value: 1 },
  { label: '最近两天', value: 2 },
  { label: '最近三天', value: 3 },
  { label: '最近五天', value: 5 },
]

const totalTokens = computed(() => (dashboard.value?.inputTokens ?? 0) + (dashboard.value?.outputTokens ?? 0))
const topTokenModels = computed(() => [...(dashboard.value?.models ?? [])]
  .sort((left, right) => (right.inputTokens + right.outputTokens) - (left.inputTokens + left.outputTokens)
    || right.requests - left.requests
    || left.name.localeCompare(right.name))
  .slice(0, 4))
const topCostModels = computed(() => (dashboard.value?.models ?? []).slice(0, 5))
const topCostRatios = computed(() => dashboard.value?.costRatios ?? [])
const selectedRangeLabel = computed(() => timeRangeOptions.find((option) => option.value === selectedRangeDays.value)?.label ?? '当前')
const availableChannels = computed(() => channels.value.filter((channel) => (
  channel.enabled && (!channel.circuitOpenUntil || Date.parse(channel.circuitOpenUntil) <= Date.now())
)).length)
const trendDimensionOptions: Array<{ label: string; value: TrendDimension }> = [
  { label: '小时', value: 'hour' },
  { label: '天', value: 'day' },
]
const requestTrendPoints = computed(() => trendDimension.value === 'hour'
  ? (dashboard.value?.hourly ?? []).map((hour) => ({
      key: hour.hour,
      label: formatHour(hour.hour),
      requests: hour.requests,
      successes: hour.successes,
    }))
  : (dashboard.value?.daily ?? []).map((day) => ({
      key: day.date,
      label: formatDate(day.date),
      requests: day.requests,
      successes: day.successes,
    })))
const requestTrendAriaLabel = computed(() => `${selectedRangeLabel.value}请求${trendDimension.value === 'hour' ? '小时' : '天'}维度折线图`)
const readyChannels = computed(() => channels.value.filter((channel) => channel.enabled && (!channel.circuitOpenUntil || Date.parse(channel.circuitOpenUntil) <= Date.now())))
const readyModels = computed(() => models.value.filter((model) => model.enabled && readyChannels.value.some((channel) => channel.models.some((mapping) => mapping.enabled && mapping.modelId === model.id))))
const readyTokens = computed(() => tokens.value.filter((token) => token.enabled))
const codexConfig = computed(() => `model_provider = "custom"
model = "${selectedModel.value || defaultCodexModel}"
network_access = "enabled"
windows_wsl_setup_acknowledged = true
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
personality = "pragmatic"

[model_providers]
[model_providers.custom]
name = "custom"
wire_api = "responses"
requires_openai_auth = true
base_url = "${serviceBaseUrl.value.trim() || '/v1'}"`)

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function formatRatio(value: number): string {
  return `${value.toFixed(2)}x`
}

function formatDate(value: string): string {
  const date = new Date(`${value}T00:00:00Z`)
  return `${date.getUTCMonth() + 1}/${date.getUTCDate()}`
}

function formatHour(value: string): string {
  const hour = value.slice(11, 13)
  if (selectedRangeDays.value === 1) return `${hour}:00`
  return `${Number(value.slice(5, 7))}/${Number(value.slice(8, 10))} ${hour}:00`
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [summary, channelItems, modelItems, tokenItems] = await Promise.all([
      request<DashboardSummary>(`/admin/gateway/dashboard?days=${selectedRangeDays.value}`),
      request<Channel[]>('/admin/gateway/channels'),
      request<GatewayModel[]>('/admin/gateway/models'),
      request<ClientToken[]>('/admin/gateway/tokens'),
    ])
    dashboard.value = summary
    channels.value = channelItems
    models.value = modelItems
    tokens.value = tokenItems
    if (!readyModels.value.some((model) => model.name === selectedModel.value)) {
      selectedModel.value = readyModels.value.find((model) => model.name === defaultCodexModel)?.name
        ?? readyModels.value[0]?.name
        ?? modelItems.find((model) => model.enabled)?.name
        ?? ''
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '仪表盘加载失败'
  } finally {
    loading.value = false
  }
}

function handleTimeRangeChange() {
  trendDimension.value = selectedRangeDays.value === 1 ? 'hour' : 'day'
  void loadDashboard()
}

async function copyCodexConfig() {
  copyingConfig.value = true
  try {
    await navigator.clipboard.writeText(codexConfig.value)
    ElMessage.success('Codex 配置已复制')
  } catch {
    ElMessage.error('无法访问剪贴板')
  } finally {
    copyingConfig.value = false
  }
}

onMounted(loadDashboard)
</script>

<template>
  <div class="page-stack dashboard-page">
    <header class="page-heading">
      <div>
        <h1>运行总览</h1>
        <p>按所选自然日范围汇总请求、Token、费用与运行质量</p>
      </div>
      <div class="page-actions">
        <el-segmented
          v-model="selectedRangeDays"
          :options="timeRangeOptions"
          :disabled="loading"
          size="small"
          aria-label="统计时间范围"
          @change="handleTimeRangeChange"
        />
        <el-tooltip content="刷新运行总览" placement="bottom">
          <el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新运行总览" @click="loadDashboard" />
        </el-tooltip>
      </div>
    </header>

    <div v-if="errorMessage" class="state-panel state-error" role="alert">
      <strong>无法读取网关统计</strong>
      <span>{{ errorMessage }}</span>
      <el-button @click="loadDashboard">重试</el-button>
    </div>

    <template v-else>
      <section class="metric-strip" aria-label="网关指标">
        <article class="metric-cell">
          <span><Tickets />请求量</span>
          <strong v-if="!loading">{{ formatCompactNumber(dashboard?.requests ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ selectedRangeLabel }}聚合统计</small>
        </article>
        <article class="metric-cell">
          <span><DataLine />成功率</span>
          <strong v-if="!loading">{{ formatPercent(dashboard?.successRate ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>最终状态为 2xx</small>
        </article>
        <el-tooltip placement="bottom" :disabled="loading || topTokenModels.length === 0" popper-class="dashboard-metric-popper">
          <template #content>
            <div class="metric-tooltip" aria-label="Token 用量最高的四个模型">
              <header><strong>模型 Token 用量</strong><span>按输入与输出合计降序</span></header>
              <ol>
                <li v-for="(model, index) in topTokenModels" :key="model.name">
                  <span class="metric-tooltip-rank">{{ index + 1 }}</span>
                  <code>{{ model.name }}</code>
                  <div>
                    <strong>{{ formatCompactNumber(model.inputTokens + model.outputTokens) }}</strong>
                    <small>输入 {{ formatCompactNumber(model.inputTokens) }} · 输出 {{ formatCompactNumber(model.outputTokens) }}</small>
                  </div>
                </li>
              </ol>
            </div>
          </template>
          <article class="metric-cell metric-cell-tooltip" tabindex="0">
            <span><Coin />Token</span>
            <strong v-if="!loading">{{ formatCompactNumber(totalTokens) }}</strong>
            <el-skeleton v-else :rows="1" animated />
            <small>悬浮查看模型用量前四</small>
          </article>
        </el-tooltip>
        <article class="metric-cell">
          <span><DataLine />缓存命中率</span>
          <strong v-if="!loading">{{ formatPercent(dashboard?.cacheHitRate ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>缓存读 {{ formatCompactNumber(dashboard?.cachedTokens ?? 0) }} · 写 {{ formatCompactNumber(dashboard?.cacheWriteTokens ?? 0) }}</small>
        </article>
        <el-tooltip placement="bottom" :disabled="loading || topCostModels.length === 0" popper-class="dashboard-metric-popper">
          <template #content>
            <div class="cost-tooltip-list" role="list" aria-label="费用最高的五个模型">
              <div v-for="model in topCostModels" :key="model.name" role="listitem"><code>{{ model.name }}</code><strong>{{ formatUSD(model.upstreamCostMicros) }}</strong></div>
            </div>
          </template>
          <article class="metric-cell metric-cell-tooltip" tabindex="0">
            <span><Coin />上游费用</span>
            <strong v-if="!loading">{{ formatUSD(dashboard?.upstreamCostMicros ?? 0) }}</strong>
            <el-skeleton v-else :rows="1" animated />
            <small>悬浮查看费用最高的模型</small>
          </article>
        </el-tooltip>
        <el-tooltip placement="bottom" :disabled="loading || topCostRatios.length === 0" popper-class="dashboard-metric-popper">
          <template #content>
            <div class="metric-tooltip cost-ratio-tooltip" aria-label="使用最多的五个费用倍率">
              <header><strong>费用倍率分布</strong><span>按可计算官方基准的请求数排序</span></header>
              <ol>
                <li v-for="(item, index) in topCostRatios" :key="item.ratio">
                  <span class="metric-tooltip-rank">{{ index + 1 }}</span>
                  <code>{{ formatRatio(item.ratio) }}</code>
                  <div>
                    <strong>{{ formatPercent(item.share) }}</strong>
                    <small>{{ formatCompactNumber(item.requests) }} 个请求</small>
                  </div>
                </li>
              </ol>
            </div>
          </template>
          <article class="metric-cell metric-cell-tooltip" tabindex="0">
            <span><Coin />费用倍率</span>
            <strong v-if="!loading">{{ formatRatio(dashboard?.upstreamCostRatio ?? 0) }}</strong>
            <el-skeleton v-else :rows="1" animated />
            <small>悬浮查看倍率占比前五</small>
          </article>
        </el-tooltip>
        <article class="metric-cell">
          <span><Timer />平均首 Token</span>
          <strong v-if="!loading">{{ dashboard?.firstTokenSampleCount ? formatDuration(dashboard.averageFirstTokenMs) : '--' }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ formatCompactNumber(dashboard?.firstTokenSampleCount ?? 0) }} 个流式样本</small>
        </article>
        <article class="metric-cell">
          <span><Odometer />平均请求延迟</span>
          <strong v-if="!loading">{{ dashboard?.latencySampleCount ? formatDuration(dashboard.averageLatencyMs) : '--' }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ formatCompactNumber(dashboard?.latencySampleCount ?? 0) }} 个响应头样本</small>
        </article>
        <article class="metric-cell">
          <span><Clock />平均请求耗时</span>
          <strong v-if="!loading">{{ formatDuration(dashboard?.averageDurationMs ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ formatCompactNumber(dashboard?.durationSampleCount ?? 0) }} 个完整请求</small>
        </article>
        <article class="metric-cell">
          <span><Connection />可用渠道</span>
          <strong v-if="!loading">{{ availableChannels }} / {{ channels.length }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ availableChannels ? '至少一个渠道可调度' : '当前无可用渠道' }}</small>
        </article>
      </section>
      <p class="historical-cost-note">{{ selectedRangeLabel }}按东八区自然日和请求级记录统计；重试不会重复累计 Token 与费用，费用优先采用最终上游返回值。</p>

      <section class="surface-panel quick-start-panel">
        <header class="panel-heading quick-start-heading">
          <button
            class="quick-start-toggle"
            type="button"
            :aria-expanded="quickStartExpanded"
            aria-controls="codex-quick-start-content"
            @click="quickStartExpanded = !quickStartExpanded"
          >
            <el-icon :class="{ 'is-expanded': quickStartExpanded }"><ArrowDown /></el-icon>
            <span><strong>Codex 快速开始</strong><small>使用 Responses 接口连接当前网关</small></span>
          </button>
          <el-button :icon="CopyDocument" :loading="copyingConfig" :disabled="!selectedModel" @click="copyCodexConfig">复制配置</el-button>
        </header>
        <el-collapse-transition>
          <div v-show="quickStartExpanded" id="codex-quick-start-content">
            <div class="readiness-strip" aria-label="Codex 接入状态">
              <div><span>渠道</span><strong>{{ readyChannels.length }} / {{ channels.length }}</strong><el-tag :type="readyChannels.length ? 'success' : 'warning'" effect="plain">{{ readyChannels.length ? '就绪' : '待配置' }}</el-tag></div>
              <div><span>模型</span><strong>{{ readyModels.length }} / {{ models.length }}</strong><el-tag :type="readyModels.length ? 'success' : 'warning'" effect="plain">{{ readyModels.length ? '就绪' : '待启用映射' }}</el-tag></div>
              <div><span>令牌</span><strong>{{ readyTokens.length }} / {{ tokens.length }}</strong><el-tag :type="readyTokens.length ? 'success' : 'warning'" effect="plain">{{ readyTokens.length ? '就绪' : '待签发' }}</el-tag></div>
            </div>
            <div class="quick-start-body">
              <div class="quick-start-fields">
                <label><span>Codex 模型</span><el-select v-model="selectedModel" filterable placeholder="选择已就绪模型"><el-option v-for="model in readyModels" :key="model.id" :label="model.name" :value="model.name" /></el-select></label>
                <label><span>服务地址</span><el-input v-model="serviceBaseUrl" /></label>
                <div class="token-safety"><strong>env_key</strong><code>OPENAI_API_KEY</code><span>令牌只写入本机环境变量，不在此处显示。</span></div>
              </div>
              <pre>{{ codexConfig }}</pre>
            </div>
          </div>
        </el-collapse-transition>
      </section>

      <div v-if="!loading && dashboard?.requests === 0" class="state-panel state-empty">
        <Connection />
        <strong>还没有网关调用</strong>
        <span>配置渠道、公开模型和访问令牌后，请求统计会显示在这里。</span>
        <el-button type="primary" @click="router.push('/channels')">配置渠道</el-button>
      </div>

      <template v-else>
        <section class="surface-panel usage-panel">
          <header class="panel-heading">
            <div>
              <h2>{{ selectedRangeLabel }}请求</h2>
              <p>{{ trendDimension === 'hour' ? '每小时' : '每日' }}请求量与成功量</p>
            </div>
            <el-segmented v-model="trendDimension" :options="trendDimensionOptions" size="small" aria-label="请求趋势统计维度" />
          </header>
          <div v-if="loading" class="chart-skeleton"><el-skeleton :rows="5" animated /></div>
          <RequestTrendChart v-else :points="requestTrendPoints" :aria-label="requestTrendAriaLabel" />
          <div class="chart-legend"><span><i class="legend-total"></i>请求</span><span><i class="legend-success"></i>成功</span></div>
        </section>

        <div class="dashboard-tables">
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>渠道分布</h2><p>{{ selectedRangeLabel }}，每个请求按最终渠道统计一次</p></div></header>
            <el-table :data="dashboard?.channels ?? []" empty-text="暂无渠道调用">
              <el-table-column prop="name" label="渠道" min-width="140" />
              <el-table-column label="请求" width="78" align="right"><template #default="scope">{{ formatCompactNumber(scope.row.requests) }}</template></el-table-column>
              <el-table-column label="Token" min-width="142" align="right">
                <template #default="scope"><div class="usage-cell"><strong>{{ formatCompactNumber(scope.row.inputTokens + scope.row.outputTokens) }}</strong><small>输入 {{ formatCompactNumber(scope.row.inputTokens) }} · 输出 {{ formatCompactNumber(scope.row.outputTokens) }}</small></div></template>
              </el-table-column>
              <el-table-column label="成功率" width="112" align="right">
                <template #default="scope"><div class="success-cell"><strong>{{ formatPercent(scope.row.successRate) }}</strong><small>{{ formatCompactNumber(scope.row.successes) }} / {{ formatCompactNumber(Math.max(0, scope.row.requests - scope.row.canceledCount)) }} 完成</small></div></template>
              </el-table-column>
              <el-table-column label="费用" width="150" align="right">
                <template #default="scope"><div class="cost-cell"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template>
              </el-table-column>
            </el-table>
          </section>
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>模型分布</h2><p>{{ selectedRangeLabel }}，按公开模型统计</p></div></header>
            <el-table :data="dashboard?.models ?? []" empty-text="暂无模型调用">
              <el-table-column prop="name" label="模型" min-width="140" />
              <el-table-column label="请求" width="78" align="right"><template #default="scope">{{ formatCompactNumber(scope.row.requests) }}</template></el-table-column>
              <el-table-column label="Token" min-width="142" align="right">
                <template #default="scope"><div class="usage-cell"><strong>{{ formatCompactNumber(scope.row.inputTokens + scope.row.outputTokens) }}</strong><small>输入 {{ formatCompactNumber(scope.row.inputTokens) }} · 输出 {{ formatCompactNumber(scope.row.outputTokens) }}</small></div></template>
              </el-table-column>
              <el-table-column label="成功率" width="112" align="right">
                <template #default="scope"><div class="success-cell"><strong>{{ formatPercent(scope.row.successRate) }}</strong><small>{{ formatCompactNumber(scope.row.successes) }} / {{ formatCompactNumber(Math.max(0, scope.row.requests - scope.row.canceledCount)) }} 完成</small></div></template>
              </el-table-column>
              <el-table-column label="费用" width="150" align="right">
                <template #default="scope"><div class="cost-cell"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template>
              </el-table-column>
            </el-table>
          </section>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.dashboard-page { min-width: 0; }
.usage-panel { min-height: 300px; }
.historical-cost-note { margin: -8px 0 0; color: var(--rose-text-subtle); font-size: 11px; }
.metric-cell-tooltip { cursor: help; }
:global(.dashboard-metric-popper) { max-width: min(440px, 90vw); }
.metric-tooltip { width: min(360px, 82vw); }
.metric-tooltip > header { display: grid; gap: 2px; padding: 3px 4px 8px; border-bottom: 1px solid rgb(255 255 255 / 16%); }
.metric-tooltip > header strong { font-size: 12px; }
.metric-tooltip > header span { color: rgb(255 255 255 / 70%); font-size: 10px; }
.metric-tooltip ol { display: grid; max-height: 244px; margin: 0; padding: 0; overflow-y: auto; list-style: none; }
.metric-tooltip li { display: grid; grid-template-columns: 20px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 8px 4px; border-bottom: 1px solid rgb(255 255 255 / 16%); }
.metric-tooltip li:last-child { border-bottom: 0; }
.metric-tooltip-rank { display: grid; width: 18px; height: 18px; place-items: center; border: 1px solid rgb(255 255 255 / 24%); border-radius: 3px; font: 600 10px/1 var(--rose-font-mono); }
.metric-tooltip code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.metric-tooltip li > div { display: grid; justify-items: end; gap: 1px; font-variant-numeric: tabular-nums; }
.metric-tooltip li small { color: rgb(255 255 255 / 70%); font-size: 9px; white-space: nowrap; }
.cost-tooltip-list { display: grid; min-width: 260px; max-height: 220px; overflow-y: auto; }
.cost-tooltip-list > div { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 18px; padding: 8px 4px; border-bottom: 1px solid rgb(255 255 255 / 16%); }
.cost-tooltip-list > div:last-child { border-bottom: 0; }
.cost-tooltip-list code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cost-tooltip-list strong { font-variant-numeric: tabular-nums; }
.quick-start-panel { overflow: hidden; }
.quick-start-heading { padding-left: 10px; }
.quick-start-toggle { display: flex; flex: 1; align-items: center; gap: 10px; min-width: 0; padding: 6px 8px; border: 0; border-radius: var(--rose-radius-control); background: transparent; color: inherit; text-align: left; cursor: pointer; }
.quick-start-toggle:focus-visible { outline: 2px solid var(--rose-primary); outline-offset: 1px; }
.quick-start-toggle .el-icon { flex: none; color: var(--rose-text-muted); transition: transform 160ms ease; }
.quick-start-toggle .el-icon.is-expanded { transform: rotate(180deg); }
.quick-start-toggle > span { display: grid; min-width: 0; gap: 2px; }
.quick-start-toggle strong { color: var(--rose-text); font-size: 14px; font-weight: 650; }
.quick-start-toggle small { color: var(--rose-text-muted); font-size: 11px; }
.readiness-strip { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-block: 1px solid var(--rose-border); }
.readiness-strip > div { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: 4px 12px; padding: 12px 16px; border-right: 1px solid var(--rose-border); }
.readiness-strip > div:last-child { border-right: 0; }
.readiness-strip span { color: var(--rose-text-muted); font-size: 11px; }
.readiness-strip strong { color: var(--rose-text); font-family: var(--rose-font-mono); }
.readiness-strip .el-tag { grid-column: 2; grid-row: 1 / span 2; }
.quick-start-body { display: grid; grid-template-columns: minmax(280px, .75fr) minmax(420px, 1.25fr); gap: 18px; padding: 18px; }
.quick-start-fields { display: grid; align-content: start; gap: 14px; }
.quick-start-fields label { display: grid; gap: 6px; }
.quick-start-fields label > span { color: var(--rose-text-muted); font-size: 11px; font-weight: 600; }
.quick-start-fields .el-select { width: 100%; }
.token-safety { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; padding: 12px; border: 1px solid var(--rose-border); background: var(--rose-surface-muted); }
.token-safety strong, .token-safety span { color: var(--rose-text-muted); font-size: 11px; }
.token-safety code { color: var(--rose-text); }
.token-safety span { grid-column: 1 / -1; }
.quick-start-body pre { min-height: 220px; margin: 0; padding: 15px; overflow: auto; border: 1px solid var(--rose-border); background: var(--rose-surface-muted); color: var(--rose-text); font: 12px/1.65 var(--rose-font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.cost-cell, .usage-cell, .success-cell { display: grid; gap: 2px; font-variant-numeric: tabular-nums; }
.cost-cell strong, .usage-cell strong, .success-cell strong { color: var(--rose-text); font-weight: 650; }
.cost-cell small, .usage-cell small, .success-cell small { color: var(--rose-text-muted); font-size: 10px; white-space: nowrap; }
.chart-skeleton { padding: 24px; }
.chart-legend { display: flex; justify-content: flex-end; gap: 18px; padding: 0 22px 18px; color: var(--rose-text-muted); font-size: 12px; }
.chart-legend span { display: inline-flex; align-items: center; gap: 6px; }
.chart-legend i { width: 10px; height: 10px; }
.legend-total { background: var(--rose-primary); }
.legend-success { background: var(--rose-success); }
.dashboard-tables { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
@media (max-width: 860px) {
  .dashboard-tables { grid-template-columns: 1fr; }
  .quick-start-body { grid-template-columns: 1fr; }
}
@media (max-width: 560px) { .readiness-strip { grid-template-columns: 1fr; } .readiness-strip > div { border-right: 0; border-bottom: 1px solid var(--rose-border); } .readiness-strip > div:last-child { border-bottom: 0; } .quick-start-body { padding: 12px; } }
</style>
