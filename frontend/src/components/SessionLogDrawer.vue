<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { ArrowDown, ArrowUp, EditPen, Refresh, Right, View } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import RequestPayloadDialog from '@/components/RequestPayloadDialog.vue'
import RouteDecisionPanel from '@/components/RouteDecisionPanel.vue'
import SessionAttemptCard from '@/components/SessionAttemptCard.vue'
import type { CodexSessionDetail, CodexSessionSummary, RelayAttemptLog, RelayRequestLog } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatCompactNumber, formatDuration } from '@/utils/formatters'

interface SessionLogDrawerProps {
  /** Session aggregate selected from the session log table. */
  summary: CodexSessionSummary | null
}

interface ChannelSwitch {
  from: string
  to: string
  reason: string
  detail: string
}

type SessionDetailStatus = 'all' | 'success' | 'canceled' | 'failure'

const { summary } = defineProps<SessionLogDrawerProps>()
const emit = defineEmits<{
  /** Notify the session list after the drawer has fully closed. */
  closed: []
}>()
const open = defineModel<boolean>({ required: true })
const loading = ref(false)
const timelineLoading = ref(false)
const loadingMore = ref(false)
const errorMessage = ref('')
const timelineErrorMessage = ref('')
const detail = ref<CodexSessionDetail | null>(null)
const pagination = ref({ page: 1, pageSize: 25 })
const payloadDialogOpen = ref(false)
const selectedRequest = ref<RelayRequestLog | null>(null)
const payloadLoadingId = ref('')
const detailStatus = ref<SessionDetailStatus>('all')
const expandedRequestIds = ref<Set<string>>(new Set())
const timelineScroller = useTemplateRef<HTMLElement>('timelineScroller')
const detailStatusOptions: Array<{ label: string; value: SessionDetailStatus }> = [
  { label: '全部', value: 'all' },
  { label: '成功', value: 'success' },
  { label: '取消', value: 'canceled' },
  { label: '失败', value: 'failure' },
]

async function renameCurrentSession() {
  if (!summary) return
  try {
    const result = await ElMessageBox.prompt('输入新的会话名称，最多 80 个字符', '修改会话名称', {
      confirmButtonText: '保存',
      cancelButtonText: '取消',
      inputValue: detail.value?.summary.sessionName || summary.sessionName,
      inputValidator: (value) => {
        const title = value.trim()
        if (!title) return '会话名称不能为空'
        if ([...title].length > 80) return '会话名称最多 80 个字符'
        return true
      },
    })
    await request<null>('/admin/gateway/sessions/title', {
      method: 'PUT',
      body: JSON.stringify({
        sessionId: summary.identified ? summary.sessionId : '',
        requestId: summary.identified ? '' : summary.fallbackRequestId,
        tokenId: summary.tokenId,
        title: result.value,
      }),
    })
    const normalized = result.value.trim().replace(/\s+/g, ' ')
    summary.sessionName = normalized
    if (detail.value) detail.value.summary.sessionName = normalized
    ElMessage.success('会话名称已保存')
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error instanceof Error ? error.message : '会话名称保存失败')
  }
}

const drawerTitle = computed(() => summary?.sessionName || (summary?.identified ? `会话 ${summary.sessionId}` : `未识别请求 ${summary?.fallbackRequestId ?? ''}`))
const timelineRequests = computed(() => (detail.value?.requests ?? []).map((requestItem, requestIndex, requests) => ({
  request: requestItem,
  attempts: requestItem.attempts.map((attempt, attemptIndex) => ({
    attempt,
    channelSwitch: resolveChannelSwitch(requests, requestIndex, attemptIndex),
  })),
})))
const hasMoreRequests = computed(() => (detail.value?.requests.length ?? 0) < (detail.value?.requestTotal ?? 0))
const failureCount = computed(() => Math.max(0, (detail.value?.summary.requestCount ?? 0) - (detail.value?.summary.successCount ?? 0) - (detail.value?.summary.canceledCount ?? 0)))

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium', timeZone: 'Asia/Shanghai' }).format(new Date(value))
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function formatTiming(value: number): string {
  return value > 0 ? formatDuration(value) : '--'
}

function statusType(requestItem: RelayRequestLog): 'success' | 'warning' | 'danger' | 'info' {
  if (requestItem.outcome === 'success' || (!requestItem.outcome && requestItem.statusCode >= 200 && requestItem.statusCode < 300)) return 'success'
  if (requestItem.outcome === 'canceled' || requestItem.statusCode === 499 || requestItem.statusCode === 408 || requestItem.statusCode === 429) return 'warning'
  if (requestItem.outcome === 'failed' || requestItem.statusCode >= 500 || requestItem.statusCode === 0) return 'danger'
  return 'info'
}

function statusLabel(requestItem: RelayRequestLog): string {
  if (requestItem.outcome === 'success' || (!requestItem.outcome && requestItem.statusCode >= 200 && requestItem.statusCode < 300)) return `成功 · HTTP ${requestItem.statusCode}`
  if (requestItem.outcome === 'canceled' || requestItem.statusCode === 499) return '客户端取消'
  if (requestItem.statusCode > 0) return `HTTP ${requestItem.statusCode}`
  if (requestItem.errorCode === 'gateway_preparation_error') return '准备失败'
  return '网络错误'
}

function channelLabel(attempt: RelayAttemptLog): string {
  return attempt.channelName || (attempt.channelId > 0 ? `渠道 #${attempt.channelId}` : '未知渠道')
}

function sameChannel(left: RelayAttemptLog, right: RelayAttemptLog): boolean {
  if (left.channelId > 0 && right.channelId > 0) return left.channelId === right.channelId
  return channelLabel(left) === channelLabel(right)
}

function selectionReasonLabel(attempt: RelayAttemptLog): string {
  switch (attempt.selectionReason) {
    case 'channel_disabled': return '原渠道已被手动停用'
    case 'mapping_disabled': return '原渠道的模型映射已停用'
    case 'circuit_open': return '原渠道处于熔断状态'
    case 'affinity_target_missing': return '会话固定的渠道或映射已被删除'
    case 'retryable_status': return '上次调用返回可重试状态'
    case 'transport_error': return '上次调用发生网络或传输错误'
    case 'response_error': return '读取上游响应失败'
    case 'upstream_application_error': return '上游返回业务中断，自动切换渠道'
    case 'gateway_preparation_error': return '网关准备上游请求失败'
    case 'circuit_opened': return '连续失败触发渠道熔断'
    case 'response_affinity': return '沿用响应固定渠道'
    case 'session_affinity': return '沿用会话固定渠道'
    default: return '首次路由选择'
  }
}

function selectionDetailLabel(attempt: RelayAttemptLog): string {
  if (!attempt.selectionDetail) return ''
  if (attempt.selectionReason === 'circuit_open' || attempt.selectionReason === 'circuit_opened') {
    const timestamp = Date.parse(attempt.selectionDetail)
    return Number.isNaN(timestamp) ? attempt.selectionDetail : `预计 ${formatDate(attempt.selectionDetail)} 恢复`
  }
  const labels: Record<string, string> = {
    upstream_request: '上游请求传输',
    response_body_read: '响应正文读取',
    stream_first_event: '流式首事件读取',
    credential_decrypt: '渠道凭据解密',
    payload_transform: '请求体转换',
    request_build: '上游请求构建',
  }
  return labels[attempt.selectionDetail] ?? attempt.selectionDetail
}

function inferredFailureReason(attempt: RelayAttemptLog): string {
  if (attempt.statusCode === 408 || attempt.statusCode === 429 || attempt.statusCode >= 500) {
    return `上次调用返回可重试状态（HTTP ${attempt.statusCode}）`
  }
  if (attempt.statusCode === 0) return '上次调用失败，网关改用其他渠道'
  return `上次调用失败（HTTP ${attempt.statusCode}）`
}

function lastAttempt(requestItem: RelayRequestLog | undefined): RelayAttemptLog | undefined {
  return requestItem?.attempts[requestItem.attempts.length - 1]
}

function resolveChannelSwitch(requests: RelayRequestLog[], requestIndex: number, attemptIndex: number): ChannelSwitch | null {
  const requestItem = requests[requestIndex]
  const attempt = requestItem?.attempts[attemptIndex]
  if (!requestItem || !attempt) return null
  const sameRequestPrevious = attemptIndex > 0 ? requestItem.attempts[attemptIndex - 1] : undefined
  const previousRequestAttempt = attemptIndex === 0 ? lastAttempt(requests[requestIndex + 1]) : undefined
  const adjacentPrevious = sameRequestPrevious ?? previousRequestAttempt

  let from = attempt.previousChannelName || (attempt.previousChannelId > 0 ? `渠道 #${attempt.previousChannelId}` : '')
  if (!from && adjacentPrevious) from = channelLabel(adjacentPrevious)
  if (!from) return null
  const to = channelLabel(attempt)
  const metadataMatchesCurrent = attempt.previousChannelId > 0 && attempt.channelId > 0
    ? attempt.previousChannelId === attempt.channelId
    : from === to
  if (metadataMatchesCurrent) return null

  if (attempt.selectionReason) {
    return { from, to, reason: selectionReasonLabel(attempt), detail: selectionDetailLabel(attempt) }
  }
  if (sameRequestPrevious && !sameRequestPrevious.success && !sameChannel(sameRequestPrevious, attempt)) {
    return { from, to, reason: inferredFailureReason(sameRequestPrevious), detail: '根据同一请求内的失败尝试推断' }
  }
  if (previousRequestAttempt && !sameChannel(previousRequestAttempt, attempt)) {
    return { from, to, reason: '历史记录未保留切换原因', detail: '' }
  }
  return null
}

function assignmentLabel(value: string): string {
  if (value === 'session_affinity') return '会话固定渠道'
  if (value === 'latest_successful_attempt') return '最近成功渠道'
  return '最近尝试渠道'
}

function migrationReasonLabel(value: string): string {
  const labels: Record<string, string> = {
    retryable_status: '上次调用返回可重试状态',
    transport_error: '上次调用发生传输错误',
    response_error: '读取上游响应失败',
    upstream_application_error: '上游业务中断',
    circuit_opened: '原渠道触发熔断',
    affinity_target_missing: '原会话渠道不可用',
    channel_disabled: '原渠道已停用',
    mapping_disabled: '原模型映射已停用',
    circuit_open: '原渠道熔断中',
  }
  return labels[value] ?? (value || '路由重新选择')
}

function currentChannelState(): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } {
  const channel = detail.value?.summary.currentChannel
  if (!channel) return { label: '未分配', type: 'info' }
  if (!channel.enabled || !channel.mappingEnabled) return { label: '已停用', type: 'warning' }
  if (channel.circuitOpenUntil && Date.parse(channel.circuitOpenUntil) > Date.now()) return { label: '熔断中', type: 'danger' }
  return { label: '可用', type: 'success' }
}

async function showParameters(requestId: string) {
  if (payloadLoadingId.value) return
  payloadLoadingId.value = requestId
  try {
    selectedRequest.value = await request<RelayRequestLog>(`/admin/gateway/logs/${encodeURIComponent(requestId)}`)
    payloadDialogOpen.value = true
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '调用详情加载失败')
  } finally {
    payloadLoadingId.value = ''
  }
}

async function refreshDetail() {
  if (loading.value || timelineLoading.value || loadingMore.value) return
  pagination.value.page = 1
  await loadDetail()
}

function handleDrawerClosed() {
  emit('closed')
}

async function requestDetailPage(page: number): Promise<CodexSessionDetail> {
  const currentSummary = summary
  if (!currentSummary) throw new Error('会话信息不存在')
  const query = new URLSearchParams({
    page: String(page),
    pageSize: String(pagination.value.pageSize),
  })
  if (currentSummary.identified) {
    query.set('sessionId', currentSummary.sessionId)
    query.set('tokenId', String(currentSummary.tokenId))
  } else {
    query.set('requestId', currentSummary.fallbackRequestId)
  }
  if (detailStatus.value !== 'all') query.set('status', detailStatus.value)
  return request<CodexSessionDetail>(`/admin/gateway/sessions/detail?${query}`)
}

async function loadDetail(append = false) {
  if (!summary) return
  if (append) loadingMore.value = true
  else loading.value = true
  errorMessage.value = ''
  const requestedPage = append ? pagination.value.page + 1 : pagination.value.page
  try {
    const result = await requestDetailPage(requestedPage)
    if (append && detail.value) {
      const existingIds = new Set(detail.value.requests.map((item) => item.id))
      detail.value.requests.push(...result.requests.filter((item) => !existingIds.has(item.id)))
      detail.value.requestTotal = result.requestTotal
      detail.value.summary = result.summary
      pagination.value.page = requestedPage
    } else {
      detail.value = result
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '会话详情加载失败'
  } finally {
    if (append) loadingMore.value = false
    else loading.value = false
  }
}

async function filterDetailByStatus() {
  if (!summary || loading.value || timelineLoading.value || loadingMore.value) return
  pagination.value.page = 1
  expandedRequestIds.value = new Set()
  timelineLoading.value = true
  timelineErrorMessage.value = ''
  try {
    const result = await requestDetailPage(1)
    if (detail.value) {
      detail.value.requests = result.requests
      detail.value.requestTotal = result.requestTotal
    } else {
      detail.value = result
    }
    await nextTick()
    timelineScroller.value?.scrollTo({ top: 0 })
  } catch (error) {
    timelineErrorMessage.value = error instanceof Error ? error.message : '调用时间线加载失败'
  } finally {
    timelineLoading.value = false
  }
}

function toggleRequest(requestId: string) {
  const next = new Set(expandedRequestIds.value)
  if (next.has(requestId)) next.delete(requestId)
  else next.add(requestId)
  expandedRequestIds.value = next
}

function isRequestExpanded(requestId: string): boolean {
  return expandedRequestIds.value.has(requestId)
}

function selectDetailStatus(status: SessionDetailStatus) {
  if (detailStatus.value === status || loading.value || timelineLoading.value || loadingMore.value) return
  detailStatus.value = status
  void filterDetailByStatus()
}

function handleTimelineScroll(event: Event) {
  const target = event.currentTarget as HTMLElement
  if (!hasMoreRequests.value || loading.value || timelineLoading.value || loadingMore.value) return
  if (target.scrollTop + target.clientHeight >= target.scrollHeight - 180) void loadDetail(true)
}

async function scrollTimeline(edge: 'top' | 'bottom') {
  if (timelineLoading.value) return
  if (edge === 'bottom' && hasMoreRequests.value && !loadingMore.value) await loadDetail(true)
  await nextTick()
  timelineScroller.value?.scrollTo({ top: edge === 'top' ? 0 : timelineScroller.value.scrollHeight, behavior: 'smooth' })
}

watch(
  () => [open.value, summary?.sessionId, summary?.fallbackRequestId, summary?.tokenId],
  ([isOpen], previous) => {
    if (!isOpen) return
    const identityChanged = !previous || previous[0] !== true || previous[1] !== summary?.sessionId || previous[2] !== summary?.fallbackRequestId || previous[3] !== summary?.tokenId
    if (identityChanged) {
      pagination.value.page = 1
      detailStatus.value = 'all'
      detail.value = null
      selectedRequest.value = null
      expandedRequestIds.value = new Set()
    }
    void loadDetail()
  },
)
</script>

<template>
  <el-drawer v-model="open" class="session-detail-drawer" size="min(1180px, 100vw)" destroy-on-close @closed="handleDrawerClosed">
    <template #header>
      <div class="drawer-title">
        <div class="drawer-title-main"><strong>{{ drawerTitle }}</strong><el-tooltip content="修改会话名称" placement="bottom"><el-button text :icon="EditPen" aria-label="修改会话名称" @click="renameCurrentSession" /></el-tooltip></div>
        <el-tooltip content="刷新会话数据" placement="bottom"><el-button class="drawer-refresh-button" text :icon="Refresh" :loading="loading || timelineLoading || loadingMore" aria-label="刷新会话数据" @click="refreshDetail" /></el-tooltip>
      </div>
    </template>
    <el-skeleton v-if="loading && !detail" :rows="8" animated />
    <div v-else-if="errorMessage" class="state-panel state-error" role="alert">
      <strong>会话详情加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadDetail">重试</el-button>
    </div>
    <div v-else-if="detail" v-loading="loading" class="session-detail">
      <section class="session-summary-strip" aria-label="会话统计">
        <div><span>请求</span><strong>{{ formatCompactNumber(detail.summary.requestCount) }}</strong></div>
        <div><span>成功率</span><strong>{{ formatPercent(detail.summary.successRate) }}</strong></div>
        <div><span>取消</span><strong>{{ formatCompactNumber(detail.summary.canceledCount) }}</strong></div>
        <div><span>平均首 Token</span><strong>{{ detail.summary.firstTokenSampleCount ? formatTiming(detail.summary.averageFirstTokenMs) : '--' }}</strong></div>
        <div><span>平均请求延迟</span><strong>{{ detail.summary.latencySampleCount ? formatTiming(detail.summary.averageLatencyMs) : '--' }}</strong></div>
        <div><span>平均请求耗时</span><strong>{{ formatTiming(detail.summary.averageDurationMs) }}</strong></div>
        <div><span>普通输入 Token</span><strong>{{ formatCompactNumber(detail.summary.normalInputTokens) }}</strong></div>
        <div><span>输出 Token</span><strong>{{ formatCompactNumber(detail.summary.outputTokens) }}</strong></div>
        <div><span>缓存读 Token</span><strong>{{ formatCompactNumber(detail.summary.cachedTokens) }}</strong></div>
        <div><span>缓存写 Token</span><strong>{{ formatCompactNumber(detail.summary.cacheWriteTokens) }}</strong></div>
        <div><span>真实发送（本地分词）</span><strong>{{ formatCompactNumber(detail.summary.sentTokens) }}</strong></div>
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
        <div v-if="detail.summary.currentChannel?.migrationHistory?.length" class="channel-migration-history">
          <div class="migration-heading"><strong>渠道迁移历史</strong><span>迁移后由接班渠道继续处理，直到接班渠道不可用</span></div>
          <ol>
            <li v-for="migration in detail.summary.currentChannel.migrationHistory" :key="`${migration.requestId}-${migration.occurredAt}-${migration.toChannelId}`">
              <button type="button" class="migration-record" :aria-label="`查看 ${formatDate(migration.occurredAt)} 的调用详情`" @click="showParameters(migration.requestId)">
                <time :datetime="migration.occurredAt">{{ formatDate(migration.occurredAt) }}</time>
                <div class="migration-route"><strong>{{ migration.fromChannelName || `渠道 #${migration.fromChannelId}` }}</strong><el-icon><Right /></el-icon><strong>{{ migration.toChannelName || `渠道 #${migration.toChannelId}` }}</strong></div>
                <div class="migration-reason"><span>{{ migrationReasonLabel(migration.reason) }}</span><small v-if="migration.detail">{{ migration.detail }}</small></div>
                <el-icon class="migration-view-icon" :class="{ 'is-loading': payloadLoadingId === migration.requestId }"><Refresh v-if="payloadLoadingId === migration.requestId" /><View v-else /></el-icon>
              </button>
            </li>
          </ol>
        </div>
      </section>

      <section class="timeline-section" aria-label="会话调用时间线">
        <header class="timeline-heading">
          <div><h3>调用时间线</h3><p>按调用发生时间从新到旧排列</p></div>
          <div class="timeline-heading-actions">
            <div class="timeline-status-tabs" role="tablist" aria-label="筛选调用状态">
              <button
                v-for="option in detailStatusOptions"
                :key="option.value"
                type="button"
                role="tab"
                :aria-selected="detailStatus === option.value"
                :class="{ 'is-active': detailStatus === option.value }"
                :disabled="loading || timelineLoading || loadingMore"
                @click="selectDetailStatus(option.value)"
              >
                <span>{{ option.label }}</span>
                <i v-if="option.value === 'canceled'">{{ formatCompactNumber(detail.summary.canceledCount) }}</i>
                <i v-else-if="option.value === 'failure'">{{ formatCompactNumber(failureCount) }}</i>
              </button>
            </div>
            <span>{{ formatCompactNumber(detail.requestTotal) }} 个匹配请求 · 会话共 {{ formatCompactNumber(detail.summary.attemptCount) }} 次上游尝试</span>
          </div>
        </header>
        <div v-loading="timelineLoading" class="timeline-list-shell">
          <div v-if="!timelineErrorMessage" class="timeline-scroll-tools" aria-label="时间线快捷滚动">
            <el-tooltip content="回到顶部" placement="left"><el-button :icon="ArrowUp" circle aria-label="回到时间线顶部" @click="scrollTimeline('top')" /></el-tooltip>
            <el-tooltip content="前往底部" placement="left"><el-button :icon="ArrowDown" circle :loading="loadingMore" aria-label="前往时间线底部" @click="scrollTimeline('bottom')" /></el-tooltip>
          </div>
          <div v-if="timelineErrorMessage" class="timeline-filter-error" role="alert"><span>{{ timelineErrorMessage }}</span><el-button :loading="timelineLoading" @click="filterDetailByStatus">重试</el-button></div>
          <div v-else ref="timelineScroller" class="timeline-scroller" @scroll="handleTimelineScroll">
          <div v-if="timelineRequests.length === 0" class="timeline-empty">当前筛选条件下没有调用记录</div>
          <ol v-else class="request-timeline">
          <li v-for="(entry, requestIndex) in timelineRequests" :key="entry.request.id" class="request-event">
            <div class="request-marker" aria-hidden="true">{{ requestIndex + 1 }}</div>
            <article class="request-content" :class="{ 'is-expanded': isRequestExpanded(entry.request.id) }">
              <header class="request-header" role="button" tabindex="0" :aria-expanded="isRequestExpanded(entry.request.id)" @click="toggleRequest(entry.request.id)" @keydown.enter.prevent="toggleRequest(entry.request.id)" @keydown.space.prevent="toggleRequest(entry.request.id)">
                <div class="request-title">
                  <el-icon class="request-expand-icon"><Right /></el-icon>
                  <time :datetime="entry.request.createdAt">{{ formatDate(entry.request.createdAt) }}</time>
                  <span>{{ entry.request.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</span>
                  <code>{{ entry.request.apiPath }}</code>
                  <code>{{ entry.request.requestedModel }}</code>
                  <span>思考等级 {{ entry.request.reasoningEffort || '默认' }}</span>
                </div>
                <div class="request-actions">
                  <el-tag :type="statusType(entry.request)" effect="plain">{{ statusLabel(entry.request) }}</el-tag>
                  <span>{{ entry.request.attemptCount }} 次尝试</span>
                  <el-tooltip content="查看完整请求与响应" placement="top">
                    <el-button class="icon-action" text :icon="View" :loading="payloadLoadingId === entry.request.id" aria-label="查看完整请求与响应" @click.stop="showParameters(entry.request.id)" />
                  </el-tooltip>
                </div>
              </header>
              <div v-if="isRequestExpanded(entry.request.id)" class="request-expanded">
              <div class="request-id"><code>{{ entry.request.id }}</code></div>
              <dl class="request-timings">
                <div><dt>首 Token</dt><dd>{{ formatTiming(entry.request.firstTokenMs) }}</dd></div>
                <div><dt>请求延迟</dt><dd>{{ formatTiming(entry.request.latencyMs) }}</dd></div>
                <div><dt>请求耗时</dt><dd>{{ formatTiming(entry.request.durationMs) }}</dd></div>
              </dl>

              <div v-if="entry.attempts.length === 0" class="route-stage-failure">
                <strong>请求未进入上游渠道</strong>
                <span>{{ entry.request.errorCode || '路由或网关准备阶段结束' }}</span>
              </div>

              <div v-for="(attemptEntry, attemptIndex) in entry.attempts" :key="attemptEntry.attempt.id" class="attempt-sequence">
                <RouteDecisionPanel v-if="attemptEntry.attempt.routeDecision" :decision="attemptEntry.attempt.routeDecision" />
                <div v-if="attemptEntry.channelSwitch" class="channel-switch-event">
                  <div class="switch-route">
                    <strong>{{ attemptEntry.channelSwitch.from }}</strong>
                    <el-icon><Right /></el-icon>
                    <strong>{{ attemptEntry.channelSwitch.to }}</strong>
                  </div>
                  <div class="switch-reason">
                    <span>{{ attemptEntry.channelSwitch.reason }}</span>
                    <small v-if="attemptEntry.channelSwitch.detail">{{ attemptEntry.channelSwitch.detail }}</small>
                  </div>
                </div>

                <SessionAttemptCard
                  :attempt="attemptEntry.attempt"
                  :attempt-number="attemptIndex + 1"
                  :api-path="attemptEntry.attempt.apiPath || entry.request.apiPath"
                  :reasoning-effort="entry.request.reasoningEffort"
                />
              </div>
              </div>
            </article>
          </li>
          </ol>
          <div v-if="loadingMore" class="timeline-loading"><el-icon class="is-loading"><Refresh /></el-icon><span>正在加载更多调用</span></div>
          <div v-else-if="timelineRequests.length && !hasMoreRequests" class="timeline-end">已加载全部 {{ formatCompactNumber(detail.requestTotal) }} 条调用</div>
          </div>
        </div>
      </section>
    </div>
  </el-drawer>

  <RequestPayloadDialog v-model="payloadDialogOpen" :request="selectedRequest" />
</template>

<style scoped>
:global(.session-detail-drawer > .el-drawer__body) { min-height: 0; overflow: hidden; }
.session-detail { display: grid; height: 100%; min-height: 0; grid-template-rows: auto auto minmax(220px, 1fr); gap: 18px; overflow: hidden; }
.drawer-title, .drawer-title-main { display: flex; align-items: center; min-width: 0; }
.drawer-title { flex: 1; justify-content: space-between; gap: 16px; }
.drawer-title-main { gap: 8px; }
.drawer-title strong { overflow: hidden; color: var(--rose-text); text-overflow: ellipsis; white-space: nowrap; }
.drawer-refresh-button { flex: 0 0 auto; width: 34px; height: 34px; padding: 0; }
.session-summary-strip { display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); border-block: 1px solid var(--rose-border); }
.session-summary-strip > div { display: grid; gap: 5px; padding: 13px 14px; border-right: 1px solid var(--rose-border); }
.session-summary-strip > div:last-child { border-right: 0; }
.session-summary-strip span, .current-channel-grid span { color: var(--rose-text-muted); font-size: 11px; }
.session-summary-strip strong { color: var(--rose-text); font-size: 15px; font-variant-numeric: tabular-nums; }
.current-channel-section { border: 1px solid var(--rose-border); border-radius: var(--rose-radius-panel); background: var(--rose-surface); }
.current-channel-section { padding: 16px; }
.current-channel-section > header { display: flex; align-items: center; justify-content: space-between; gap: 18px; }
.current-channel-section h3, .timeline-heading h3 { font-size: 14px; }
.current-channel-section p, .timeline-heading p { margin-top: 3px; color: var(--rose-text-muted); font-size: 11px; }
.current-channel-grid { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 14px 20px; margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--rose-border); }
.current-channel-grid > div { display: grid; gap: 4px; min-width: 0; }
.channel-url { grid-column: 1 / -1; }
.current-channel-grid code, .request-timeline code { overflow-wrap: anywhere; }
.channel-migration-history { margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--rose-border); }
.migration-heading { display: flex; align-items: baseline; gap: 10px; margin-bottom: 10px; }
.migration-heading strong { color: var(--rose-text); font-size: 12px; }
.migration-heading span { color: var(--rose-text-muted); font-size: 10px; }
.channel-migration-history ol { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
.channel-migration-history li { min-width: 0; }
.migration-record { display: grid; width: 100%; grid-template-columns: 125px minmax(220px, auto) minmax(0, 1fr) 24px; align-items: center; gap: 12px; padding: 8px 10px; border: 0; border-left: 3px solid var(--rose-warning); color: inherit; background: var(--rose-warning-soft); text-align: left; cursor: pointer; }
.migration-record:hover, .migration-record:focus-visible { background: color-mix(in srgb, var(--rose-warning-soft) 82%, var(--rose-warning)); outline: none; }
.migration-record:focus-visible { box-shadow: inset 0 0 0 2px var(--rose-warning); }
.channel-migration-history time { color: var(--rose-text-muted); font-size: 10px; font-variant-numeric: tabular-nums; }
.migration-route, .migration-reason { display: flex; align-items: center; gap: 7px; min-width: 0; }
.migration-route strong { overflow: hidden; color: var(--rose-text); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.migration-route .el-icon { flex: 0 0 auto; color: var(--rose-warning); }
.migration-reason { flex-wrap: wrap; color: var(--rose-warning); font-size: 11px; }
.migration-reason small { color: var(--rose-text-muted); }
.migration-view-icon { justify-self: end; color: var(--rose-text-muted); }
.timeline-section { display: grid; min-width: 0; min-height: 0; grid-template-rows: auto minmax(0, 1fr); }
.timeline-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 18px; padding-bottom: 12px; border-bottom: 1px solid var(--rose-border); }
.timeline-heading-actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; }
.timeline-heading-actions > span { color: var(--rose-text-muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.timeline-status-tabs { display: flex; align-items: stretch; border: 1px solid var(--rose-border); border-radius: 4px; overflow: hidden; }
.timeline-status-tabs button { display: inline-flex; min-height: 30px; align-items: center; gap: 6px; padding: 0 10px; border: 0; border-right: 1px solid var(--rose-border); color: var(--rose-text-muted); background: var(--rose-surface); cursor: pointer; }
.timeline-status-tabs button:last-child { border-right: 0; }
.timeline-status-tabs button:hover { color: var(--rose-primary); background: var(--rose-primary-soft); }
.timeline-status-tabs button.is-active { color: var(--rose-surface); background: var(--rose-primary); }
.timeline-status-tabs button:disabled { cursor: wait; opacity: .65; }
.timeline-status-tabs i { display: inline-grid; min-width: 18px; height: 18px; padding: 0 5px; place-items: center; border-radius: 9px; color: var(--rose-danger); background: var(--rose-danger-soft); font: normal 600 10px/1 var(--rose-font-mono); }
.timeline-status-tabs button.is-active i { color: var(--rose-primary-hover); background: var(--rose-surface); }
.timeline-list-shell { position: relative; display: grid; min-width: 0; min-height: 0; grid-template-columns: minmax(0, 1fr) 44px; }
.timeline-scroller { grid-column: 1; grid-row: 1; height: 100%; min-height: 0; overflow-y: auto; padding-right: 12px; scrollbar-gutter: stable; }
.timeline-scroll-tools { z-index: 3; display: flex; grid-column: 2; grid-row: 1; align-self: stretch; align-items: center; justify-content: center; flex-direction: column; gap: 8px; border-left: 1px solid var(--rose-border); background: var(--rose-surface); }
.timeline-scroll-tools .el-button { width: 34px; height: 34px; }
.timeline-scroll-tools .el-button + .el-button { margin-left: 0; }
.timeline-filter-error { display: flex; grid-column: 1; grid-row: 1; min-height: 160px; align-items: center; justify-content: center; gap: 12px; color: var(--rose-danger); }
.timeline-empty { padding: 32px 16px; color: var(--rose-text-muted); text-align: center; }
.request-timeline { display: grid; margin: 0; padding: 0; list-style: none; }
.request-timings { display: flex; flex-wrap: wrap; gap: 8px 22px; margin: 8px 0 0; font-variant-numeric: tabular-nums; }
.request-timings > div { display: flex; align-items: baseline; gap: 6px; }
.request-timings dt { color: var(--rose-text-muted); font-size: 10px; }
.request-timings dd { margin: 0; color: var(--rose-text); font-size: 12px; font-weight: 650; }
.request-event { position: relative; display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 12px; padding: 18px 0; }
.request-event:not(:last-child)::before { position: absolute; top: 46px; bottom: -12px; left: 16px; width: 1px; background: var(--rose-border-strong); content: ''; }
.request-marker { z-index: 1; display: grid; width: 33px; height: 33px; place-items: center; border: 1px solid var(--rose-primary); border-radius: 50%; color: var(--rose-primary-hover); background: var(--rose-surface); font: 600 11px/1 var(--rose-font-mono); }
.request-content { min-width: 0; padding-bottom: 12px; border-bottom: 1px solid var(--rose-border); }
.request-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 34px; }
.request-header { padding: 5px 6px; border-radius: 3px; cursor: pointer; }
.request-header:hover, .request-header:focus-visible { background: var(--rose-surface-muted); outline: none; }
.request-title, .request-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 7px 12px; min-width: 0; }
.request-expand-icon { flex: 0 0 auto; color: var(--rose-text-subtle); transition: transform 150ms ease; }
.request-content.is-expanded .request-expand-icon { transform: rotate(90deg); }
.request-expanded { padding: 0 6px 6px; }
.request-title time { color: var(--rose-text); font-weight: 650; }
.request-title span, .request-actions > span, .request-id { color: var(--rose-text-muted); font-size: 11px; }
.request-id { margin-top: 2px; }
.request-actions { flex: 0 0 auto; justify-content: flex-end; }
.icon-action { width: 32px; height: 32px; padding: 0; }
.route-stage-failure { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 12px 14px; border-left: 3px solid var(--rose-danger); color: var(--rose-text-muted); background: var(--rose-danger-soft); }
.route-stage-failure strong { color: var(--rose-danger); }
.attempt-sequence { display: grid; gap: 9px; margin-top: 12px; }
.channel-switch-event { display: grid; grid-template-columns: minmax(220px, auto) minmax(0, 1fr); align-items: center; gap: 14px; padding: 9px 12px; border-left: 3px solid var(--rose-warning); background: var(--rose-warning-soft); }
.switch-route, .switch-reason { display: flex; align-items: center; gap: 8px; min-width: 0; }
.switch-route strong { overflow: hidden; color: var(--rose-text); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.switch-route .el-icon { flex: 0 0 auto; color: var(--rose-warning); }
.switch-reason { flex-wrap: wrap; color: var(--rose-warning); font-size: 12px; }
.switch-reason small { color: var(--rose-text-muted); }
.timeline-loading, .timeline-end { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 16px; color: var(--rose-text-muted); font-size: 11px; }
@media (max-width: 860px) { .session-summary-strip { grid-template-columns: repeat(3, 1fr); } .session-summary-strip > div:nth-child(3n) { border-right: 0; } .current-channel-grid { grid-template-columns: repeat(2, 1fr); } .migration-record { grid-template-columns: 116px minmax(0, 1fr) 24px; } .migration-reason { grid-column: 1 / -2; } .migration-view-icon { grid-column: -2 / -1; grid-row: 1; } .channel-switch-event { grid-template-columns: 1fr; gap: 4px; } }
@media (max-width: 860px) { :global(.session-detail-drawer > .el-drawer__body) { overflow-y: auto; } .session-detail { height: auto; grid-template-rows: auto; overflow: visible; } .timeline-list-shell { height: min(62dvh, 640px); min-height: 320px; } }
@media (max-width: 560px) { .session-summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); } .session-summary-strip > div:nth-child(3n) { border-right: 1px solid var(--rose-border); } .session-summary-strip > div:nth-child(even), .session-summary-strip > div:last-child { border-right: 0; } .current-channel-grid { grid-template-columns: 1fr; } .current-channel-section > header, .timeline-heading, .request-header { align-items: flex-start; flex-direction: column; } .timeline-heading-actions { width: 100%; align-items: flex-start; flex-direction: column; } .request-event { grid-template-columns: 26px minmax(0, 1fr); gap: 8px; } .request-event:not(:last-child)::before { left: 12px; } .request-marker { width: 25px; height: 25px; font-size: 10px; } .request-actions { width: 100%; justify-content: flex-start; } .channel-switch-event { padding: 9px; } .switch-route { flex-wrap: wrap; } .route-stage-failure { align-items: flex-start; flex-direction: column; } }
</style>
