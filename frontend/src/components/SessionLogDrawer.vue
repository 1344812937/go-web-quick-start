<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Right, View } from '@element-plus/icons-vue'
import type { CodexSessionDetail, CodexSessionSummary, RelayAttemptLog, RelayRequestLog } from '@/types/gateway'
import { request } from '@/utils/api'

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

const { summary } = defineProps<SessionLogDrawerProps>()
const open = defineModel<boolean>({ required: true })
const loading = ref(false)
const errorMessage = ref('')
const detail = ref<CodexSessionDetail | null>(null)
const pagination = ref({ page: 1, pageSize: 25 })
const parameterDialogOpen = ref(false)
const selectedRequest = ref<RelayRequestLog | null>(null)

const drawerTitle = computed(() => summary?.identified ? `会话 ${summary.sessionId}` : `未识别请求 ${summary?.fallbackRequestId ?? ''}`)
const timelineRequests = computed(() => (detail.value?.requests ?? []).map((requestItem, requestIndex, requests) => ({
  request: requestItem,
  attempts: requestItem.attempts.map((attempt, attemptIndex) => ({
    attempt,
    channelSwitch: resolveChannelSwitch(requests, requestIndex, attemptIndex),
  })),
})))

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

function statusLabel(status: number, errorMessage = ''): string {
  if (status > 0) return String(status)
  if (errorMessage.startsWith('gateway preparation failed:')) return '准备失败'
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
  const previousRequestAttempt = attemptIndex === 0 ? lastAttempt(requests[requestIndex - 1]) : undefined
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

      <section class="timeline-section" aria-label="会话调用时间线">
        <header class="timeline-heading">
          <div><h3>调用时间线</h3><p>按调用发生时间从旧到新排列</p></div>
          <span>{{ detail.requestTotal }} 个请求 · {{ detail.summary.attemptCount }} 次上游尝试</span>
        </header>
        <div v-if="timelineRequests.length === 0" class="timeline-empty">当前页没有调用记录</div>
        <ol v-else class="request-timeline">
          <li v-for="(entry, requestIndex) in timelineRequests" :key="entry.request.id" class="request-event">
            <div class="request-marker" aria-hidden="true">{{ (pagination.page - 1) * pagination.pageSize + requestIndex + 1 }}</div>
            <article class="request-content">
              <header class="request-header">
                <div class="request-title">
                  <time :datetime="entry.request.createdAt">{{ formatDate(entry.request.createdAt) }}</time>
                  <span>{{ entry.request.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</span>
                  <code>{{ entry.request.requestedModel }}</code>
                </div>
                <div class="request-actions">
                  <el-tag :type="statusType(entry.request.statusCode)" effect="plain">{{ statusLabel(entry.request.statusCode) }}</el-tag>
                  <span>{{ entry.request.attemptCount }} 次尝试</span>
                  <el-tooltip content="查看接口调用参数" placement="top">
                    <el-button class="icon-action" text :icon="View" aria-label="查看接口调用参数" @click="showParameters(entry.request)" />
                  </el-tooltip>
                </div>
              </header>
              <div class="request-id"><code>{{ entry.request.id }}</code></div>

              <div v-if="entry.attempts.length === 0" class="route-stage-failure">
                <strong>请求未进入上游渠道</strong>
                <span>{{ entry.request.errorCode || '路由或网关准备阶段结束' }}</span>
              </div>

              <div v-for="(attemptEntry, attemptIndex) in entry.attempts" :key="attemptEntry.attempt.id" class="attempt-sequence">
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

                <article class="attempt-event">
                  <div class="attempt-index">尝试 {{ attemptIndex + 1 }}</div>
                  <div class="attempt-channel">
                    <strong>{{ channelLabel(attemptEntry.attempt) }}</strong>
                    <small><code>{{ attemptEntry.attempt.upstreamModel }}</code></small>
                    <small v-if="attemptEntry.attempt.channelBaseUrl"><code>{{ attemptEntry.attempt.channelBaseUrl }}</code></small>
                  </div>
                  <el-tag :type="statusType(attemptEntry.attempt.statusCode)" effect="plain">{{ statusLabel(attemptEntry.attempt.statusCode, attemptEntry.attempt.errorMessage) }}</el-tag>
                  <dl class="attempt-metrics">
                    <div><dt>延迟</dt><dd>{{ attemptEntry.attempt.latencyMs }} ms</dd></div>
                    <div><dt>普通输入</dt><dd>{{ formatTokens(attemptEntry.attempt.normalInputTokens) }}</dd></div>
                    <div><dt>输出</dt><dd>{{ formatTokens(attemptEntry.attempt.outputTokens) }}</dd></div>
                    <div><dt>缓存读</dt><dd>{{ formatTokens(attemptEntry.attempt.cachedTokens) }}</dd></div>
                    <div><dt>缓存写</dt><dd>{{ formatTokens(attemptEntry.attempt.cacheWriteTokens) }}</dd></div>
                    <div><dt>真实发送</dt><dd>{{ formatTokens(attemptEntry.attempt.sentTokens) }}</dd></div>
                    <div><dt>上游金额</dt><dd>{{ formatUSD(attemptEntry.attempt.upstreamCostMicros) }}</dd></div>
                    <div><dt>自行估算</dt><dd>{{ formatUSD(attemptEntry.attempt.estimatedCostMicros) }}</dd></div>
                  </dl>
                  <div class="attempt-source"><el-tag :type="costSourceType(attemptEntry.attempt.costSource)" effect="plain" size="small">{{ costSourceLabel(attemptEntry.attempt.costSource) }}</el-tag></div>
                  <p v-if="attemptEntry.attempt.errorMessage" class="attempt-error">{{ attemptEntry.attempt.errorMessage }}</p>
                </article>
              </div>
            </article>
          </li>
        </ol>
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
    <template #footer><div class="dialog-actions"><el-button @click="parameterDialogOpen = false">关闭</el-button></div></template>
  </el-dialog>
</template>

<style scoped>
.session-detail { display: grid; gap: 22px; }
.session-summary-strip { display: grid; grid-template-columns: repeat(9, minmax(110px, 1fr)); border-block: 1px solid var(--rose-border); }
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
.timeline-section { min-width: 0; }
.timeline-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 18px; padding-bottom: 12px; border-bottom: 1px solid var(--rose-border); }
.timeline-heading > span { color: var(--rose-text-muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.timeline-empty { padding: 32px 16px; color: var(--rose-text-muted); text-align: center; }
.request-timeline { display: grid; margin: 0; padding: 0; list-style: none; }
.request-event { position: relative; display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 12px; padding: 18px 0; }
.request-event:not(:last-child)::before { position: absolute; top: 46px; bottom: -12px; left: 16px; width: 1px; background: var(--rose-border-strong); content: ''; }
.request-marker { z-index: 1; display: grid; width: 33px; height: 33px; place-items: center; border: 1px solid var(--rose-primary); border-radius: 50%; color: var(--rose-primary-hover); background: var(--rose-surface); font: 600 11px/1 var(--rose-font-mono); }
.request-content { min-width: 0; padding-bottom: 18px; border-bottom: 1px solid var(--rose-border); }
.request-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 34px; }
.request-title, .request-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 7px 12px; min-width: 0; }
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
.attempt-event { display: grid; grid-template-columns: 66px minmax(150px, .9fr) 96px minmax(460px, 2.2fr) 104px; align-items: center; gap: 12px; min-width: 0; padding: 12px 14px; border: 1px solid var(--rose-border); background: var(--rose-surface); }
.attempt-index { color: var(--rose-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.attempt-channel { display: grid; gap: 2px; min-width: 0; }
.attempt-channel strong { overflow: hidden; color: var(--rose-text); text-overflow: ellipsis; white-space: nowrap; }
.attempt-channel small { overflow: hidden; color: var(--rose-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.attempt-metrics { display: grid; grid-template-columns: repeat(4, minmax(74px, 1fr)); gap: 8px 12px; margin: 0; font-variant-numeric: tabular-nums; }
.attempt-metrics div { display: grid; gap: 1px; min-width: 0; }
.attempt-metrics dt { color: var(--rose-text-muted); font-size: 10px; }
.attempt-metrics dd { margin: 0; color: var(--rose-text); font-size: 12px; }
.attempt-source { justify-self: end; }
.attempt-error { grid-column: 2 / -1; margin: -2px 0 0; color: var(--rose-danger); font-size: 11px; overflow-wrap: anywhere; }
.parameter-dialog-content { display: grid; gap: 14px; }
.parameter-meta { display: flex; flex-wrap: wrap; gap: 8px 18px; color: var(--rose-text-muted); font-size: 12px; }
.parameter-meta span { display: flex; align-items: center; gap: 7px; }
.parameter-meta strong { color: var(--rose-text); }
.parameter-dialog-content pre { max-height: 56vh; margin: 0; padding: 14px; overflow: auto; border: 1px solid var(--rose-border); border-radius: var(--rose-radius-control); color: var(--rose-text); background: var(--rose-surface-muted); font: 12px/1.6 var(--rose-font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.parameter-dialog-content .mapping-empty { padding: 24px; border: 1px dashed var(--rose-border-strong); color: var(--rose-text-muted); text-align: center; }
@media (max-width: 1040px) { .attempt-event { grid-template-columns: 58px minmax(160px, 1fr) 92px; } .attempt-metrics { grid-column: 1 / -1; grid-row: 2; } .attempt-source { grid-column: 3; } .attempt-error { grid-column: 1 / -1; } }
@media (max-width: 860px) { .session-summary-strip { grid-template-columns: repeat(3, 1fr); } .session-summary-strip > div:nth-child(3n) { border-right: 0; } .current-channel-grid { grid-template-columns: repeat(2, 1fr); } .channel-switch-event { grid-template-columns: 1fr; gap: 4px; } }
@media (max-width: 560px) { .session-summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); } .session-summary-strip > div:nth-child(3n) { border-right: 1px solid var(--rose-border); } .session-summary-strip > div:nth-child(even), .session-summary-strip > div:last-child { border-right: 0; } .current-channel-grid { grid-template-columns: 1fr; } .current-channel-section > header, .timeline-heading, .request-header { align-items: flex-start; flex-direction: column; } .request-event { grid-template-columns: 26px minmax(0, 1fr); gap: 8px; } .request-event:not(:last-child)::before { left: 12px; } .request-marker { width: 25px; height: 25px; font-size: 10px; } .request-actions { width: 100%; justify-content: flex-start; } .channel-switch-event { padding: 9px; } .switch-route { flex-wrap: wrap; } .attempt-event { grid-template-columns: minmax(0, 1fr) auto; padding: 10px; } .attempt-index { grid-column: 1; } .attempt-channel { grid-column: 1 / -1; } .attempt-event > .el-tag { grid-column: 2; grid-row: 1; } .attempt-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); grid-column: 1 / -1; grid-row: auto; } .attempt-source { grid-column: 1 / -1; justify-self: start; } .attempt-error { grid-column: 1 / -1; } .route-stage-failure { align-items: flex-start; flex-direction: column; } }
</style>
