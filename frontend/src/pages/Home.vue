<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Clock, Coin, Connection, CopyDocument, DataLine, Odometer, Refresh, Tickets, Timer } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { Channel, ClientToken, DashboardSummary, GatewayModel } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatDuration } from '@/utils/formatters'

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

const totalTokens = computed(() => (dashboard.value?.inputTokens ?? 0) + (dashboard.value?.outputTokens ?? 0))
const availableChannels = computed(() => channels.value.filter((channel) => (
  channel.enabled && (!channel.circuitOpenUntil || Date.parse(channel.circuitOpenUntil) <= Date.now())
)).length)
const maxDailyRequests = computed(() => Math.max(1, ...(dashboard.value?.daily.map((day) => day.requests) ?? [1])))
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

function formatInteger(value: number): string {
  return new Intl.NumberFormat('zh-CN', { notation: value >= 100000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value)
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function formatDate(value: string): string {
  const date = new Date(`${value}T00:00:00Z`)
  return `${date.getUTCMonth() + 1}/${date.getUTCDate()}`
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [summary, channelItems, modelItems, tokenItems] = await Promise.all([
      request<DashboardSummary>('/admin/gateway/dashboard'),
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
        <p>累计令牌统计，以及最近 5 天的渠道和模型明细</p>
      </div>
      <div class="page-actions">
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
          <strong v-if="!loading">{{ formatInteger(dashboard?.requests ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>令牌聚合统计</small>
        </article>
        <article class="metric-cell">
          <span><DataLine />成功率</span>
          <strong v-if="!loading">{{ formatPercent(dashboard?.successRate ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>最终状态为 2xx</small>
        </article>
        <article class="metric-cell">
          <span><Coin />Token</span>
          <strong v-if="!loading">{{ formatInteger(totalTokens) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>输入与输出合计</small>
        </article>
        <article class="metric-cell">
          <span><Coin />上游费用</span>
          <strong v-if="!loading">{{ formatUSD(dashboard?.upstreamCostMicros ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>自行估算 {{ formatUSD(dashboard?.estimatedCostMicros ?? 0) }}</small>
        </article>
        <article class="metric-cell">
          <span><Timer />平均首 Token</span>
          <strong v-if="!loading">{{ dashboard?.firstTokenSampleCount ? formatDuration(dashboard.averageFirstTokenMs) : '--' }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ dashboard?.firstTokenSampleCount ?? 0 }} 个流式样本</small>
        </article>
        <article class="metric-cell">
          <span><Odometer />平均请求延迟</span>
          <strong v-if="!loading">{{ dashboard?.latencySampleCount ? formatDuration(dashboard.averageLatencyMs) : '--' }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ dashboard?.latencySampleCount ?? 0 }} 个响应头样本</small>
        </article>
        <article class="metric-cell">
          <span><Clock />平均请求耗时</span>
          <strong v-if="!loading">{{ formatDuration(dashboard?.averageDurationMs ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ dashboard?.durationSampleCount ?? 0 }} 个完整请求</small>
        </article>
        <article class="metric-cell">
          <span><Connection />可用渠道</span>
          <strong v-if="!loading">{{ availableChannels }} / {{ channels.length }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ availableChannels ? '至少一个渠道可调度' : '当前无可用渠道' }}</small>
        </article>
      </section>
      <p class="historical-cost-note">5 天前的历史聚合费用沿用原有估算口径；最近 5 天已按成功状态重建。</p>

      <section class="surface-panel quick-start-panel">
        <header class="panel-heading">
          <div><h2>Codex 快速开始</h2><p>使用 Responses 接口连接当前网关</p></div>
          <el-button :icon="CopyDocument" :loading="copyingConfig" :disabled="!selectedModel" @click="copyCodexConfig">复制配置</el-button>
        </header>
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
              <h2>近 14 日请求</h2>
              <p>每日请求量与成功量</p>
            </div>
          </header>
          <div v-if="loading" class="chart-skeleton"><el-skeleton :rows="5" animated /></div>
          <div v-else class="bar-chart" aria-label="近 14 日请求柱状图">
            <div v-for="day in dashboard?.daily" :key="day.date" class="bar-column">
              <div class="bar-track">
                <span class="bar-total" :style="{ height: `${Math.max(3, day.requests / maxDailyRequests * 100)}%` }"></span>
                <span class="bar-success" :style="{ height: `${Math.max(0, day.successes / maxDailyRequests * 100)}%` }"></span>
              </div>
              <small>{{ formatDate(day.date) }}</small>
            </div>
          </div>
          <div class="chart-legend"><span><i class="legend-total"></i>请求</span><span><i class="legend-success"></i>成功</span></div>
        </section>

        <div class="dashboard-tables">
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>渠道分布</h2><p>最近 5 天，按尝试次数统计</p></div></header>
            <el-table :data="dashboard?.channels ?? []" empty-text="暂无渠道调用">
              <el-table-column prop="name" label="渠道" min-width="140" />
              <el-table-column prop="requests" label="尝试" width="92" align="right" />
              <el-table-column label="费用" width="150" align="right">
                <template #default="scope"><div class="cost-cell"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template>
              </el-table-column>
            </el-table>
          </section>
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>模型分布</h2><p>最近 5 天，按公开模型统计</p></div></header>
            <el-table :data="dashboard?.models ?? []" empty-text="暂无模型调用">
              <el-table-column prop="name" label="模型" min-width="140" />
              <el-table-column prop="requests" label="请求" width="92" align="right" />
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
.quick-start-panel { overflow: hidden; }
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
.cost-cell { display: grid; gap: 2px; }
.cost-cell strong { color: var(--rose-text); font-weight: 650; }
.cost-cell small { color: var(--rose-text-muted); font-size: 10px; }
.chart-skeleton { padding: 24px; }
.bar-chart { display: grid; grid-template-columns: repeat(14, minmax(24px, 1fr)); align-items: end; gap: 8px; height: 210px; padding: 20px 22px 12px; overflow-x: auto; }
.bar-column { display: grid; grid-template-rows: 160px 22px; align-items: end; gap: 8px; min-width: 24px; text-align: center; }
.bar-track { position: relative; height: 160px; border-bottom: 1px solid var(--rose-border); background: var(--rose-surface-muted); }
.bar-track span { position: absolute; inset: auto 0 0; min-height: 0; transition: height 180ms ease; }
.bar-total { background: var(--rose-primary-soft); }
.bar-success { left: 28% !important; right: 28% !important; background: var(--rose-success); }
.bar-column small { color: var(--rose-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.chart-legend { display: flex; justify-content: flex-end; gap: 18px; padding: 0 22px 18px; color: var(--rose-text-muted); font-size: 12px; }
.chart-legend span { display: inline-flex; align-items: center; gap: 6px; }
.chart-legend i { width: 10px; height: 10px; }
.legend-total { background: var(--rose-primary-soft); }
.legend-success { background: var(--rose-success); }
.dashboard-tables { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
@media (max-width: 860px) {
  .dashboard-tables { grid-template-columns: 1fr; }
  .bar-chart { grid-template-columns: repeat(14, minmax(30px, 1fr)); }
  .quick-start-body { grid-template-columns: 1fr; }
}
@media (max-width: 560px) { .readiness-strip { grid-template-columns: 1fr; } .readiness-strip > div { border-right: 0; border-bottom: 1px solid var(--rose-border); } .readiness-strip > div:last-child { border-bottom: 0; } .quick-start-body { padding: 12px; } }
</style>
