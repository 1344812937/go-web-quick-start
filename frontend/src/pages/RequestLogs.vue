<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Coin, Connection, DataLine, Refresh, RefreshLeft, Search, Tickets, Timer, View } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import RequestPayloadDialog from '@/components/RequestPayloadDialog.vue'
import SessionAttemptCard from '@/components/SessionAttemptCard.vue'
import type { Channel, ClientToken, GatewayModel, LogAggregateSummary, LogPage, RelayRequestLog } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatCompactNumber, formatDuration } from '@/utils/formatters'
import { logDateDefaultTimes, logDateRangeShortcuts, todayLogRange } from '@/utils/logDateRanges'

const loading = ref(true)
const errorMessage = ref('')
const logs = ref<RelayRequestLog[]>([])
const summary = ref<LogAggregateSummary | null>(null)
const total = ref(0)
const models = ref<GatewayModel[]>([])
const channels = ref<Channel[]>([])
const tokens = ref<ClientToken[]>([])
const filters = reactive({ model: '', channelId: '', tokenId: '', status: '', range: todayLogRange() as Date[] | null })
const pagination = reactive({ page: 1, pageSize: 50 })
const payloadDialogOpen = ref(false)
const selectedRequest = ref<RelayRequestLog | null>(null)
const payloadLoadingId = ref('')

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function formatTiming(value: number): string {
  return value > 0 ? formatDuration(value) : '--'
}

function formatAverageTiming(value: number, samples: number): string {
  return samples > 0 ? formatDuration(value) : '--'
}

function tokenName(log: RelayRequestLog): string {
  return log.tokenName || tokens.value.find((token) => token.id === log.tokenId)?.name || `令牌 #${log.tokenId}`
}

function usageSource(value: string): string {
  if (value === 'upstream') return '上游 usage'
  if (value === 'estimated_tiktoken') return '本地 tiktoken 估算'
  if (value === 'mixed') return '混合来源'
  return '未知'
}

function costSource(log: RelayRequestLog): string {
  if (log.costSource === 'upstream') return '上游返回'
  if (log.costSource === 'estimated_fallback') return '估算回退'
  if (log.costSource === 'mixed') return '混合'
  if (log.outcome === 'canceled') return '取消时费用未知'
  return '失败为零'
}

function costSourceType(log: RelayRequestLog): 'success' | 'warning' | 'danger' | 'info' {
  if (log.costSource === 'upstream') return 'success'
  if (log.costSource === 'estimated_fallback' || log.costSource === 'mixed' || log.outcome === 'canceled') return 'warning'
  return 'info'
}

function statusType(status: number): 'success' | 'warning' | 'danger' | 'info' {
  if (status >= 200 && status < 300) return 'success'
  if (status === 408 || status === 429) return 'warning'
  if (status >= 500 || status === 0) return 'danger'
  return 'info'
}

function outcomeLabel(log: RelayRequestLog): string {
  if (log.outcome === 'success' || (log.outcome === '' && log.statusCode >= 200 && log.statusCode < 300)) return `HTTP ${log.statusCode}`
  if (log.outcome === 'canceled' || log.statusCode === 499) return '客户端取消'
  return log.statusCode > 0 ? `HTTP ${log.statusCode}` : '网络错误'
}

function outcomeType(log: RelayRequestLog): 'success' | 'warning' | 'danger' | 'info' {
  if (log.outcome === 'success' || (log.outcome === '' && log.statusCode >= 200 && log.statusCode < 300)) return 'success'
  if (log.outcome === 'canceled' || log.statusCode === 499) return 'warning'
  return statusType(log.statusCode)
}

async function showPayloads(log: RelayRequestLog) {
  payloadLoadingId.value = log.id
  try {
    selectedRequest.value = await request<RelayRequestLog>(`/admin/gateway/logs/${encodeURIComponent(log.id)}`)
    payloadDialogOpen.value = true
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '调用详情加载失败')
  } finally {
    payloadLoadingId.value = ''
  }
}

async function loadOptions() {
  [models.value, channels.value, tokens.value] = await Promise.all([
    request<GatewayModel[]>('/admin/gateway/models'),
    request<Channel[]>('/admin/gateway/channels'),
    request<ClientToken[]>('/admin/gateway/tokens'),
  ])
}

async function loadLogs() {
  loading.value = true
  errorMessage.value = ''
  const query = new URLSearchParams({ page: String(pagination.page), pageSize: String(pagination.pageSize) })
  if (filters.model) query.set('model', filters.model)
  if (filters.channelId) query.set('channelId', filters.channelId)
  if (filters.tokenId) query.set('tokenId', filters.tokenId)
  if (filters.status) query.set('status', filters.status)
  if (filters.range?.length === 2) {
    query.set('from', filters.range[0].toISOString())
    query.set('to', filters.range[1].toISOString())
  }
  try {
    const page = await request<LogPage>(`/admin/gateway/logs?${query}`)
    logs.value = page.items
    summary.value = page.summary
    total.value = page.total
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '调用日志加载失败'
  } finally {
    loading.value = false
  }
}

function searchLogs() {
  pagination.page = 1
  void loadLogs()
}

function resetLogs() {
  Object.assign(filters, { model: '', channelId: '', tokenId: '', status: '', range: todayLogRange() })
  pagination.page = 1
  void loadLogs()
}

onMounted(async () => {
  try {
    await loadOptions()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '筛选项加载失败'
  }
  await loadLogs()
})
</script>

<template>
  <div class="page-stack">
    <header class="page-heading">
      <div><h1>调用日志</h1><p>默认显示今天，可查询 5 天内的请求状态、重试尝试与费用</p></div>
      <div class="page-actions"><el-tooltip content="刷新调用日志" placement="bottom"><el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新调用日志" @click="loadLogs" /></el-tooltip></div>
    </header>

    <section class="filter-bar" aria-label="日志筛选">
      <el-select v-model="filters.model" clearable placeholder="全部模型"><el-option v-for="model in models" :key="model.id" :label="model.name" :value="model.name" /></el-select>
      <el-select v-model="filters.channelId" clearable placeholder="全部渠道"><el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="String(channel.id)" /></el-select>
      <el-select v-model="filters.tokenId" clearable placeholder="全部令牌"><el-option v-for="token in tokens" :key="token.id" :label="token.name" :value="String(token.id)" /></el-select>
      <el-select v-model="filters.status" clearable placeholder="全部状态"><el-option label="成功 2xx" value="200" /><el-option label="客户端取消 499" value="499" /><el-option label="限流 429" value="429" /><el-option label="服务不可用 503" value="503" /></el-select>
      <el-date-picker v-model="filters.range" type="datetimerange" format="YYYY-MM-DD HH:mm:ss" :default-time="logDateDefaultTimes" :shortcuts="logDateRangeShortcuts" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
      <div class="filter-actions"><el-button :icon="RefreshLeft" :disabled="loading" @click="resetLogs">重置</el-button><el-button type="primary" :icon="Search" :loading="loading" @click="searchLogs">查询</el-button></div>
    </section>

    <section v-if="!errorMessage" class="metric-strip" aria-label="调用日志汇总">
      <article class="metric-cell">
        <span><Tickets />请求量</span>
        <strong v-if="!loading">{{ formatCompactNumber(summary?.requestCount ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>当前筛选范围</small>
      </article>
      <article class="metric-cell">
        <span><DataLine />成功率</span>
        <strong v-if="!loading">{{ formatPercent(summary?.successRate ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>成功 {{ formatCompactNumber(summary?.successCount ?? 0) }} · 取消 {{ formatCompactNumber(summary?.canceledCount ?? 0) }} · 失败 {{ formatCompactNumber((summary?.requestCount ?? 0) - (summary?.successCount ?? 0) - (summary?.canceledCount ?? 0)) }}</small>
      </article>
      <article class="metric-cell">
        <span><Connection />上游尝试</span>
        <strong v-if="!loading">{{ formatCompactNumber(summary?.attemptCount ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>包含重试调用</small>
      </article>
      <article class="metric-cell">
        <span><Coin />Token</span>
        <strong v-if="!loading">{{ formatCompactNumber((summary?.inputTokens ?? 0) + (summary?.outputTokens ?? 0)) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>输入 {{ formatCompactNumber(summary?.inputTokens ?? 0) }} · 输出 {{ formatCompactNumber(summary?.outputTokens ?? 0) }}</small>
      </article>
      <article class="metric-cell">
        <span><Coin />上游费用</span>
        <strong v-if="!loading">{{ formatUSD(summary?.upstreamCostMicros ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>自行估算 {{ formatUSD(summary?.estimatedCostMicros ?? 0) }}</small>
      </article>
      <article class="metric-cell">
        <span><Timer />平均请求耗时</span>
        <strong v-if="!loading">{{ formatAverageTiming(summary?.averageDurationMs ?? 0, summary?.durationSampleCount ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>首 Token {{ formatAverageTiming(summary?.averageFirstTokenMs ?? 0, summary?.firstTokenSampleCount ?? 0) }} · 延迟 {{ formatAverageTiming(summary?.averageLatencyMs ?? 0, summary?.latencySampleCount ?? 0) }}</small>
      </article>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>调用日志加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadLogs">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="logs" row-key="id" empty-text="当前筛选条件下没有调用日志">
        <el-table-column type="expand">
          <template #default="scope">
            <div class="attempt-list">
              <header><strong>上游尝试</strong><span>共 {{ scope.row.attempts.length }} 次</span></header>
              <div v-if="scope.row.attempts.length === 0" class="muted-text">请求未进入上游调度</div>
              <SessionAttemptCard
                v-for="(attempt, index) in scope.row.attempts"
                :key="attempt.id"
                :attempt="attempt"
                :attempt-number="index + 1"
                :api-path="attempt.apiPath || scope.row.apiPath"
                :reasoning-effort="scope.row.reasoningEffort"
              />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="时间 / 请求 ID" min-width="220"><template #default="scope"><div class="primary-cell"><strong>{{ formatDate(scope.row.createdAt) }}</strong><small><code>{{ scope.row.id }}</code></small></div></template></el-table-column>
        <el-table-column label="会话 / 调用令牌" min-width="210"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.sessionName || scope.row.codexSessionId || '未命名会话' }}</strong><small>{{ tokenName(scope.row) }} · <code>{{ scope.row.tokenKeyPrefix || '无历史前缀' }}</code></small></div></template></el-table-column>
        <el-table-column label="接口 / 模型" min-width="200"><template #default="scope"><div class="primary-cell"><strong><code>{{ scope.row.apiPath }}</code></strong><small>{{ scope.row.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }} · <code>{{ scope.row.requestedModel }}</code></small></div></template></el-table-column>
        <el-table-column label="状态" width="108"><template #default="scope"><el-tag :type="outcomeType(scope.row)" effect="plain">{{ outcomeLabel(scope.row) }}</el-tag></template></el-table-column>
        <el-table-column label="Token 明细" min-width="300">
          <template #default="scope">
            <div class="token-breakdown">
              <span><small>普通输入</small><strong>{{ formatCompactNumber(scope.row.normalInputTokens) }}</strong></span>
              <span><small>输出</small><strong>{{ formatCompactNumber(scope.row.outputTokens) }}</strong></span>
              <span><small>缓存读</small><strong>{{ formatCompactNumber(scope.row.cachedTokens) }}</strong></span>
              <span><small>缓存写</small><strong>{{ formatCompactNumber(scope.row.cacheWriteTokens) }}</strong></span>
              <span class="sent-token"><small>真实发送（本地分词）</small><strong>{{ formatCompactNumber(scope.row.sentTokens) }}</strong></span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="费用" width="158" align="right"><template #default="scope"><div class="cost-breakdown"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>自行估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template></el-table-column>
        <el-table-column label="来源" width="142"><template #default="scope"><div class="source-breakdown"><el-tag :type="costSourceType(scope.row)" effect="plain" size="small">{{ costSource(scope.row) }}</el-tag><small>{{ usageSource(scope.row.usageSource) }}</small></div></template></el-table-column>
        <el-table-column label="首 Token" width="104" align="right"><template #default="scope">{{ formatTiming(scope.row.firstTokenMs) }}</template></el-table-column>
        <el-table-column label="请求延迟" width="104" align="right"><template #default="scope">{{ formatTiming(scope.row.latencyMs) }}</template></el-table-column>
        <el-table-column label="请求耗时" width="104" align="right"><template #default="scope">{{ formatTiming(scope.row.durationMs) }}</template></el-table-column>
        <el-table-column label="尝试" width="72" align="right" prop="attemptCount" />
        <el-table-column label="详情" width="62" fixed="right" align="right"><template #default="scope"><el-tooltip content="查看完整请求与响应" placement="top"><el-button class="table-action-button" text :icon="View" :loading="payloadLoadingId === scope.row.id" aria-label="查看完整请求与响应" @click="showPayloads(scope.row)" /></el-tooltip></template></el-table-column>
      </el-table>
      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :disabled="loading" :total="total" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadLogs" /></footer>
    </section>
    <RequestPayloadDialog v-model="payloadDialogOpen" :request="selectedRequest" />
  </div>
</template>

<style scoped>
.attempt-list { display: grid; gap: 8px; padding: 14px 24px 18px 54px; background: var(--rose-surface-muted); }
.attempt-list > header { display: flex; justify-content: space-between; color: var(--rose-text-muted); font-size: 12px; }
.attempt-list > header strong { color: var(--rose-text); }
.token-breakdown { display: grid; grid-template-columns: repeat(4, minmax(58px, 1fr)); gap: 5px 10px; font-variant-numeric: tabular-nums; }
.token-breakdown > span { display: grid; gap: 1px; min-width: 0; }
.token-breakdown small { color: var(--rose-text-muted); font-size: 10px; white-space: nowrap; }
.token-breakdown strong { color: var(--rose-text); font-size: 12px; }
.token-breakdown .sent-token { grid-column: 1 / -1; padding-top: 3px; border-top: 1px solid var(--rose-border); }
.cost-breakdown, .source-breakdown { display: grid; gap: 3px; }
.cost-breakdown strong { color: var(--rose-text); font-variant-numeric: tabular-nums; }
.cost-breakdown small, .source-breakdown small { color: var(--rose-text-muted); font-size: 10px; }
.source-breakdown { justify-items: start; }
@media (max-width: 720px) { .attempt-list { padding: 10px; overflow-x: auto; } }
</style>
