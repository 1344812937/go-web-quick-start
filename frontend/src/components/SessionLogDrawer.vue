<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { View } from '@element-plus/icons-vue'
import type { CodexSessionDetail, CodexSessionSummary, RelayAttemptLog, RelayRequestLog } from '@/types/gateway'
import { request } from '@/utils/api'

interface SessionLogDrawerProps {
  /** Session aggregate selected from the session log table. */
  summary: CodexSessionSummary | null
}

interface ChannelAttemptItem {
  request: RelayRequestLog
  attempt: RelayAttemptLog
}

interface ChannelAttemptGroup {
  key: string
  channelId: number
  channelName: string
  channelBaseUrl: string
  current: boolean
  requestCount: number
  normalInputTokens: number
  outputTokens: number
  cachedTokens: number
  cacheWriteTokens: number
  sentTokens: number
  estimatedCostMicros: number
  upstreamCostMicros: number
  items: ChannelAttemptItem[]
}

const { summary } = defineProps<SessionLogDrawerProps>()
const open = defineModel<boolean>({ required: true })
const loading = ref(false)
const errorMessage = ref('')
const detail = ref<CodexSessionDetail | null>(null)
const pagination = ref({ page: 1, pageSize: 25 })
const parameterDialogOpen = ref(false)
const selectedRequest = ref<RelayRequestLog | null>(null)

const drawerTitle = computed(() => summary?.identified ? `会话 ${summary.sessionId}` : `未识别请求 ${summary?.fallbackRequestId ?? ''}`)
const channelGroups = computed<ChannelAttemptGroup[]>(() => {
  const groups = new Map<string, ChannelAttemptGroup & { requestIds: Set<string> }>()
  const currentChannelId = detail.value?.summary.currentChannel?.channelId ?? 0
  for (const requestItem of detail.value?.requests ?? []) {
    for (const attempt of requestItem.attempts) {
      const key = attempt.channelId > 0 ? String(attempt.channelId) : `${attempt.channelName}:${attempt.channelBaseUrl}`
      let group = groups.get(key)
      if (!group) {
        group = {
          key,
          channelId: attempt.channelId,
          channelName: attempt.channelName || `渠道 #${attempt.channelId}`,
          channelBaseUrl: attempt.channelBaseUrl,
          current: attempt.channelId === currentChannelId,
          requestCount: 0,
          normalInputTokens: 0,
          outputTokens: 0,
          cachedTokens: 0,
          cacheWriteTokens: 0,
          sentTokens: 0,
          estimatedCostMicros: 0,
          upstreamCostMicros: 0,
          items: [],
          requestIds: new Set<string>(),
        }
        groups.set(key, group)
      }
      group.items.push({ request: requestItem, attempt })
      group.requestIds.add(requestItem.id)
      group.requestCount = group.requestIds.size
      group.normalInputTokens += attempt.normalInputTokens
      group.outputTokens += attempt.outputTokens
      group.cachedTokens += attempt.cachedTokens
      group.cacheWriteTokens += attempt.cacheWriteTokens
      group.sentTokens += attempt.sentTokens
      group.estimatedCostMicros += attempt.estimatedCostMicros
      group.upstreamCostMicros += attempt.upstreamCostMicros
    }
  }
  return Array.from(groups.values())
    .map(({ requestIds: _requestIds, ...group }) => group)
    .sort((left, right) => Number(right.current) - Number(left.current) || left.channelName.localeCompare(right.channelName))
})
const unassignedRequests = computed(() => detail.value?.requests.filter((item) => item.attempts.length === 0) ?? [])

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('zh-CN').format(value)
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function statusType(status: number): 'success' | 'warning' | 'danger' | 'info' {
  if (status >= 200 && status < 300) return 'success'
  if (status === 408 || status === 429) return 'warning'
  if (status >= 500 || status === 0) return 'danger'
  return 'info'
}

function assignmentLabel(value: string): string {
  if (value === 'session_affinity') return '会话固定渠道'
  if (value === 'latest_successful_attempt') return '最近成功渠道'
  return '最近尝试渠道'
}

function costSourceLabel(value: RelayRequestLog['costSource']): string {
  if (value === 'upstream') return '上游返回'
  if (value === 'estimated_fallback') return '估算回退'
  if (value === 'mixed') return '混合'
  return '失败为零'
}

function costSourceType(value: RelayRequestLog['costSource']): 'success' | 'warning' | 'info' {
  if (value === 'upstream') return 'success'
  if (value === 'estimated_fallback' || value === 'mixed') return 'warning'
  return 'info'
}

function currentChannelState(): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } {
  const channel = detail.value?.summary.currentChannel
  if (!channel) return { label: '未分配', type: 'info' }
  if (!channel.enabled || !channel.mappingEnabled) return { label: '已停用', type: 'warning' }
  if (channel.circuitOpenUntil && Date.parse(channel.circuitOpenUntil) > Date.now()) return { label: '熔断中', type: 'danger' }
  return { label: '可用', type: 'success' }
}

function parameterJSON(value: Record<string, unknown>): string {
  return JSON.stringify(value, null, 2)
}

function showParameters(requestItem: RelayRequestLog) {
  selectedRequest.value = requestItem
  parameterDialogOpen.value = true
}

async function loadDetail() {
  if (!summary) return
  loading.value = true
  errorMessage.value = ''
  const query = new URLSearchParams({
    page: String(pagination.value.page),
    pageSize: String(pagination.value.pageSize),
  })
  if (summary.identified) {
    query.set('sessionId', summary.sessionId)
    query.set('tokenId', String(summary.tokenId))
  } else {
    query.set('requestId', summary.fallbackRequestId)
  }
  try {
    detail.value = await request<CodexSessionDetail>(`/admin/gateway/sessions/detail?${query}`)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '会话详情加载失败'
  } finally {
    loading.value = false
  }
}

watch(
  () => [open.value, summary?.sessionId, summary?.fallbackRequestId, summary?.tokenId],
  ([isOpen], previous) => {
    if (!isOpen) return
    const identityChanged = !previous || previous[1] !== summary?.sessionId || previous[2] !== summary?.fallbackRequestId || previous[3] !== summary?.tokenId
    if (identityChanged) {
      pagination.value.page = 1
      detail.value = null
      selectedRequest.value = null
    }
    void loadDetail()
  },
)
</script>

<template>
  <el-drawer v-model="open" :title="drawerTitle" size="min(1180px, 100vw)" destroy-on-close>
    <el-skeleton v-if="loading && !detail" :rows="8" animated />
    <div v-else-if="errorMessage" class="state-panel state-error" role="alert">
      <strong>会话详情加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadDetail">重试</el-button>
    </div>
    <div v-else-if="detail" v-loading="loading" class="session-detail">
      <section class="session-summary-strip" aria-label="会话统计">
        <div><span>请求</span><strong>{{ detail.summary.requestCount }}</strong></div>
        <div><span>成功率</span><strong>{{ formatPercent(detail.summary.successRate) }}</strong></div>
        <div><span>普通输入 Token</span><strong>{{ formatTokens(detail.summary.normalInputTokens) }}</strong></div>
        <div><span>输出 Token</span><strong>{{ formatTokens(detail.summary.outputTokens) }}</strong></div>
        <div><span>缓存读 Token</span><strong>{{ formatTokens(detail.summary.cachedTokens) }}</strong></div>
        <div><span>缓存写 Token</span><strong>{{ formatTokens(detail.summary.cacheWriteTokens) }}</strong></div>
        <div><span>真实发送（本地分词）</span><strong>{{ formatTokens(detail.summary.sentTokens) }}</strong></div>
        <div><span>上游金额</span><strong>{{ formatUSD(detail.summary.upstreamCostMicros) }}</strong></div>
        <div><span>自行估算</span><strong>{{ formatUSD(detail.summary.estimatedCostMicros) }}</strong></div>
      </section>

      <section class="current-channel-section">
        <header><div><h3>当前渠道</h3><p>{{ detail.summary.latestModel }} · {{ detail.summary.latestEndpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</p></div><el-tag :type="currentChannelState().type" effect="plain">{{ currentChannelState().label }}</el-tag></header>
        <div v-if="detail.summary.currentChannel" class="current-channel-grid">
          <div><span>渠道</span><strong>{{ detail.summary.currentChannel.channelName }}</strong></div>
          <div><span>上游模型</span><code>{{ detail.summary.currentChannel.upstreamModel }}</code></div>
          <div><span>分配依据</span><strong>{{ assignmentLabel(detail.summary.currentChannel.assignmentSource) }}</strong></div>
          <div><span>最近使用</span><strong>{{ formatDate(detail.summary.currentChannel.lastUsedAt) }}</strong></div>
          <div class="channel-url"><span>Base URL</span><code>{{ detail.summary.currentChannel.channelBaseUrl }}</code></div>
        </div>
        <div v-else class="muted-text">该会话尚未进入上游渠道</div>
      </section>

      <section v-for="group in channelGroups" :key="group.key" class="channel-group">
        <header>
          <div><div class="channel-heading"><h3>{{ group.channelName }}</h3><el-tag v-if="group.current" type="success" effect="plain">当前</el-tag></div><p><code>{{ group.channelBaseUrl }}</code></p></div>
          <div class="channel-totals"><span>{{ group.requestCount }} 个请求</span><span>普通输入 {{ formatTokens(group.normalInputTokens) }}</span><span>输出 {{ formatTokens(group.outputTokens) }}</span><span>缓存读 {{ formatTokens(group.cachedTokens) }}</span><span>缓存写 {{ formatTokens(group.cacheWriteTokens) }}</span><span>真实发送（本地分词） {{ formatTokens(group.sentTokens) }}</span><span>上游金额 {{ formatUSD(group.upstreamCostMicros) }}</span><span>自行估算 {{ formatUSD(group.estimatedCostMicros) }}</span></div>
        </header>
        <el-table :data="group.items" row-key="attempt.id" empty-text="没有调用记录">
          <el-table-column label="时间 / 请求" min-width="190"><template #default="scope"><div class="primary-cell"><strong>{{ formatDate(scope.row.request.createdAt) }}</strong><small><code>{{ scope.row.request.id }}</code></small></div></template></el-table-column>
          <el-table-column label="端点 / 模型" min-width="170"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.request.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</strong><small><code>{{ scope.row.request.requestedModel }} → {{ scope.row.attempt.upstreamModel }}</code></small></div></template></el-table-column>
          <el-table-column label="状态" width="96"><template #default="scope"><el-tag :type="statusType(scope.row.attempt.statusCode)" effect="plain">{{ scope.row.attempt.statusCode || '网络错误' }}</el-tag></template></el-table-column>
          <el-table-column label="Token 明细" min-width="310">
            <template #default="scope">
              <div class="attempt-token-grid">
                <span><small>普通输入</small><strong>{{ formatTokens(scope.row.attempt.normalInputTokens) }}</strong></span>
                <span><small>输出</small><strong>{{ formatTokens(scope.row.attempt.outputTokens) }}</strong></span>
                <span><small>缓存读</small><strong>{{ formatTokens(scope.row.attempt.cachedTokens) }}</strong></span>
                <span><small>缓存写</small><strong>{{ formatTokens(scope.row.attempt.cacheWriteTokens) }}</strong></span>
                <span><small>真实发送（本地分词）</small><strong>{{ formatTokens(scope.row.attempt.sentTokens) }}</strong></span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="延迟" width="90" align="right"><template #default="scope">{{ scope.row.attempt.latencyMs }} ms</template></el-table-column>
          <el-table-column label="费用" width="158" align="right"><template #default="scope"><div class="attempt-cost"><strong>{{ formatUSD(scope.row.attempt.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(scope.row.attempt.estimatedCostMicros) }}</small></div></template></el-table-column>
          <el-table-column label="费用来源" width="108"><template #default="scope"><el-tag :type="costSourceType(scope.row.attempt.costSource)" effect="plain" size="small">{{ costSourceLabel(scope.row.attempt.costSource) }}</el-tag></template></el-table-column>
          <el-table-column label="参数" width="76" fixed="right"><template #default="scope"><el-button text :icon="View" title="查看接口调用参数" @click="showParameters(scope.row.request)" /></template></el-table-column>
        </el-table>
      </section>

      <section v-if="unassignedRequests.length" class="channel-group">
        <header><div><h3>未分配渠道</h3><p>请求在路由阶段结束</p></div><span class="muted-text">{{ unassignedRequests.length }} 个请求</span></header>
        <el-table :data="unassignedRequests" row-key="id">
          <el-table-column label="时间 / 请求" min-width="210"><template #default="scope"><div class="primary-cell"><strong>{{ formatDate(scope.row.createdAt) }}</strong><small><code>{{ scope.row.id }}</code></small></div></template></el-table-column>
          <el-table-column label="模型" min-width="140" prop="requestedModel" />
          <el-table-column label="状态" width="96"><template #default="scope"><el-tag :type="statusType(scope.row.statusCode)" effect="plain">{{ scope.row.statusCode }}</el-tag></template></el-table-column>
          <el-table-column label="错误码" min-width="150" prop="errorCode" />
          <el-table-column label="参数" width="76"><template #default="scope"><el-button text :icon="View" title="查看接口调用参数" @click="showParameters(scope.row)" /></template></el-table-column>
        </el-table>
      </section>

      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :disabled="loading" :total="detail.requestTotal" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadDetail" /></footer>
    </div>
  </el-drawer>

  <el-dialog v-model="parameterDialogOpen" title="接口调用参数" width="min(720px, 92vw)" append-to-body>
    <div v-if="selectedRequest" class="parameter-dialog-content">
      <div class="parameter-meta"><span><strong>请求 ID</strong><code>{{ selectedRequest.id }}</code></span><span><strong>端点</strong>{{ selectedRequest.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</span><span><strong>模型</strong><code>{{ selectedRequest.requestedModel }}</code></span></div>
      <pre v-if="Object.keys(selectedRequest.requestParameters).length">{{ parameterJSON(selectedRequest.requestParameters) }}</pre>
      <div v-else class="mapping-empty">当前请求没有可展示的参数</div>
    </div>
  </el-dialog>
</template>

<style scoped>
.session-detail { display: grid; gap: 22px; }
.session-summary-strip { display: grid; grid-template-columns: repeat(9, minmax(110px, 1fr)); border-block: 1px solid var(--rose-border); }
.session-summary-strip > div { display: grid; gap: 5px; padding: 13px 14px; border-right: 1px solid var(--rose-border); }
.session-summary-strip > div:last-child { border-right: 0; }
.session-summary-strip span, .current-channel-grid span { color: var(--rose-text-muted); font-size: 11px; }
.session-summary-strip strong { color: var(--rose-text); font-size: 15px; font-variant-numeric: tabular-nums; }
.current-channel-section, .channel-group { border: 1px solid var(--rose-border); border-radius: var(--rose-radius-panel); background: var(--rose-surface); }
.current-channel-section { padding: 16px; }
.current-channel-section > header, .channel-group > header, .channel-heading, .channel-totals { display: flex; align-items: center; }
.current-channel-section > header, .channel-group > header { justify-content: space-between; gap: 18px; }
.current-channel-section h3, .channel-group h3 { font-size: 14px; }
.current-channel-section p, .channel-group p { margin-top: 3px; color: var(--rose-text-muted); font-size: 11px; }
.current-channel-grid { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 14px 20px; margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--rose-border); }
.current-channel-grid > div { display: grid; gap: 4px; min-width: 0; }
.channel-url { grid-column: 1 / -1; }
.current-channel-grid code, .channel-group code { overflow-wrap: anywhere; }
.channel-group { overflow: hidden; }
.channel-group > header { padding: 14px 16px; border-bottom: 1px solid var(--rose-border); background: var(--rose-surface-muted); }
.channel-heading { gap: 8px; }
.channel-totals { flex-wrap: wrap; justify-content: flex-end; gap: 6px 16px; color: var(--rose-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.attempt-token-grid { display: grid; grid-template-columns: repeat(5, minmax(56px, 1fr)); gap: 8px; font-variant-numeric: tabular-nums; }
.attempt-token-grid > span { display: grid; gap: 1px; }
.attempt-token-grid small { color: var(--rose-text-muted); font-size: 10px; white-space: nowrap; }
.attempt-token-grid strong { color: var(--rose-text); font-size: 12px; }
.attempt-cost { display: grid; gap: 2px; }
.attempt-cost strong { color: var(--rose-text); font-variant-numeric: tabular-nums; }
.attempt-cost small { color: var(--rose-text-muted); font-size: 10px; }
.parameter-dialog-content { display: grid; gap: 14px; }
.parameter-meta { display: flex; flex-wrap: wrap; gap: 8px 18px; color: var(--rose-text-muted); font-size: 12px; }
.parameter-meta span { display: flex; align-items: center; gap: 7px; }
.parameter-meta strong { color: var(--rose-text); }
.parameter-dialog-content pre { max-height: 56vh; margin: 0; padding: 14px; overflow: auto; border: 1px solid var(--rose-border); border-radius: var(--rose-radius-control); color: var(--rose-text); background: var(--rose-surface-muted); font: 12px/1.6 var(--rose-font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.parameter-dialog-content .mapping-empty { padding: 24px; border: 1px dashed var(--rose-border-strong); color: var(--rose-text-muted); text-align: center; }
@media (max-width: 860px) { .session-summary-strip { grid-template-columns: repeat(3, 1fr); } .session-summary-strip > div:nth-child(3n) { border-right: 0; } .current-channel-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { .session-summary-strip { grid-template-columns: repeat(2, 1fr); } .session-summary-strip > div:nth-child(3n) { border-right: 1px solid var(--rose-border); } .session-summary-strip > div:nth-child(even), .session-summary-strip > div:last-child { border-right: 0; } .current-channel-grid { grid-template-columns: 1fr; } .current-channel-section > header, .channel-group > header { align-items: flex-start; flex-direction: column; } .channel-totals { justify-content: flex-start; } }
</style>
