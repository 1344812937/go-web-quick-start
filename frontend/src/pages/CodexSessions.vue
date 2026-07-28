<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import type { CSSProperties } from 'vue'
import { Coin, Connection, DataLine, EditPen, Refresh, RefreshLeft, Search, Tickets, Timer, View } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import SessionLogDrawer from '@/components/SessionLogDrawer.vue'
import type { Channel, ClientToken, CodexSessionPage, CodexSessionSummary, GatewayModel, LogAggregateSummary } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatCompactNumber, formatDuration } from '@/utils/formatters'
import { logDateDefaultTimes, logDateRangeShortcuts, todayLogRange } from '@/utils/logDateRanges'

const loading = ref(true)
const errorMessage = ref('')
const sessions = ref<CodexSessionSummary[]>([])
const summary = ref<LogAggregateSummary | null>(null)
const total = ref(0)
const models = ref<GatewayModel[]>([])
const channels = ref<Channel[]>([])
const tokens = ref<ClientToken[]>([])
const filters = reactive({ session: '', model: '', channelId: '', tokenId: '', range: todayLogRange() as Date[] | null })
const pagination = reactive({ page: 1, pageSize: 25 })
const drawerOpen = ref(false)
const selectedSession = ref<CodexSessionSummary | null>(null)

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatTiming(value: number, samples: number): string {
  return samples > 0 ? formatDuration(value) : '--'
}

function requestCountStyle(count: number): CSSProperties {
  const normalizedCount = Math.max(1, count)
  const logCount = Math.log10(normalizedCount)
  const lowToMedium = Math.min(1, Math.max(0, logCount - 1))
  const mediumToHigh = Math.min(1, Math.max(0, (normalizedCount - 100) / 200))
  const color = normalizedCount <= 100
    ? `color-mix(in srgb, var(--supos-success) ${(1 - lowToMedium) * 100}%, var(--supos-warning))`
    : `color-mix(in srgb, var(--supos-warning) ${(1 - mediumToHigh) * 100}%, var(--supos-danger))`
  return { '--request-count-color': color } as CSSProperties
}

function sessionSourceLabel(value: string): string {
  if (value === 'prompt_cache_key') return '缓存键'
  if (value.includes('session_id')) return '客户端会话 ID'
  if (value.includes('thread_id')) return '客户端任务 ID'
  return '未识别'
}

function channelState(session: CodexSessionSummary): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } {
  const channel = session.currentChannel
  if (!channel) return { label: '未分配', type: 'info' }
  if (!channel.enabled || !channel.mappingEnabled) return { label: '已停用', type: 'warning' }
  if (channel.circuitOpenUntil && Date.parse(channel.circuitOpenUntil) > Date.now()) return { label: '熔断中', type: 'danger' }
  return { label: '当前渠道', type: 'success' }
}

async function loadOptions() {
  [models.value, channels.value, tokens.value] = await Promise.all([
    request<GatewayModel[]>('/admin/gateway/models'),
    request<Channel[]>('/admin/gateway/channels'),
    request<ClientToken[]>('/admin/gateway/tokens'),
  ])
}

async function loadSessions() {
  loading.value = true
  errorMessage.value = ''
  const query = new URLSearchParams({ page: String(pagination.page), pageSize: String(pagination.pageSize) })
  if (filters.session.trim()) query.set('session', filters.session.trim())
  if (filters.model) query.set('model', filters.model)
  if (filters.channelId) query.set('channelId', filters.channelId)
  if (filters.tokenId) query.set('tokenId', filters.tokenId)
  if (filters.range?.length === 2) {
    query.set('from', filters.range[0].toISOString())
    query.set('to', filters.range[1].toISOString())
  }
  try {
    const page = await request<CodexSessionPage>(`/admin/gateway/sessions?${query}`)
    sessions.value = page.items
    summary.value = page.summary
    total.value = page.total
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '会话日志加载失败'
  } finally {
    loading.value = false
  }
}

function searchSessions() {
  pagination.page = 1
  void loadSessions()
}

function resetSessions() {
  Object.assign(filters, { session: '', model: '', channelId: '', tokenId: '', range: todayLogRange() })
  pagination.page = 1
  void loadSessions()
}

function openSession(session: CodexSessionSummary) {
  selectedSession.value = session
  drawerOpen.value = true
}

async function renameSession(session: CodexSessionSummary) {
  try {
    const result = await ElMessageBox.prompt('输入新的会话名称，最多 80 个字符', '修改会话名称', {
      confirmButtonText: '保存',
      cancelButtonText: '取消',
      inputValue: session.sessionName,
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
        sessionId: session.identified ? session.sessionId : '',
        requestId: session.identified ? '' : session.fallbackRequestId,
        tokenId: session.tokenId,
        title: result.value,
      }),
    })
    const normalized = result.value.trim().replace(/\s+/g, ' ')
    session.sessionName = normalized
    if (selectedSession.value && sessionRowKey(selectedSession.value) === sessionRowKey(session)) {
      selectedSession.value.sessionName = normalized
    }
    ElMessage.success('会话名称已保存')
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error instanceof Error ? error.message : '会话名称保存失败')
  }
}

function sessionRowKey(session: CodexSessionSummary): string {
  return session.identified ? `${session.tokenId}:${session.sessionId}` : session.fallbackRequestId
}

onMounted(async () => {
  try {
    await loadOptions()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '筛选项加载失败'
  }
  await loadSessions()
})
</script>

<template>
  <div class="page-stack">
    <header class="page-heading">
      <div><h1>会话日志</h1><p>默认显示今天，可查询 5 天内的会话、渠道、模型、令牌与用量</p></div>
      <div class="page-actions"><el-tooltip content="刷新会话日志" placement="bottom"><el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新会话日志" @click="loadSessions" /></el-tooltip></div>
    </header>

    <section class="filter-bar" aria-label="会话日志筛选">
      <el-input v-model="filters.session" clearable placeholder="会话名称、会话 ID 或请求 ID" @keyup.enter="searchSessions" />
      <el-select v-model="filters.model" clearable placeholder="全部模型"><el-option v-for="model in models" :key="model.id" :label="model.name" :value="model.name" /></el-select>
      <el-select v-model="filters.channelId" clearable placeholder="全部渠道"><el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="String(channel.id)" /></el-select>
      <el-select v-model="filters.tokenId" clearable placeholder="全部令牌"><el-option v-for="token in tokens" :key="token.id" :label="token.name" :value="String(token.id)" /></el-select>
      <el-date-picker v-model="filters.range" type="datetimerange" format="YYYY-MM-DD HH:mm:ss" :default-time="logDateDefaultTimes" :shortcuts="logDateRangeShortcuts" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
      <div class="filter-actions"><el-button :icon="RefreshLeft" :disabled="loading" @click="resetSessions">重置</el-button><el-button type="primary" :icon="Search" :loading="loading" @click="searchSessions">查询</el-button></div>
    </section>

    <section v-if="!errorMessage" class="metric-strip" aria-label="会话日志汇总">
      <article class="metric-cell">
        <span><Tickets />会话数</span>
        <strong v-if="!loading">{{ formatCompactNumber(total) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>当前筛选范围</small>
      </article>
      <article class="metric-cell">
        <span><Connection />请求量</span>
        <strong v-if="!loading">{{ formatCompactNumber(summary?.requestCount ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>{{ formatCompactNumber(summary?.attemptCount ?? 0) }} 次上游尝试</small>
      </article>
      <article class="metric-cell">
        <span><DataLine />成功率</span>
        <strong v-if="!loading">{{ formatPercent(summary?.successRate ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>成功 {{ formatCompactNumber(summary?.successCount ?? 0) }} · 取消 {{ formatCompactNumber(summary?.canceledCount ?? 0) }} · 失败 {{ formatCompactNumber((summary?.requestCount ?? 0) - (summary?.successCount ?? 0) - (summary?.canceledCount ?? 0)) }}</small>
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
        <strong v-if="!loading">{{ formatTiming(summary?.averageDurationMs ?? 0, summary?.durationSampleCount ?? 0) }}</strong><el-skeleton v-else :rows="1" animated />
        <small>首 Token {{ formatTiming(summary?.averageFirstTokenMs ?? 0, summary?.firstTokenSampleCount ?? 0) }} · 延迟 {{ formatTiming(summary?.averageLatencyMs ?? 0, summary?.latencySampleCount ?? 0) }}</small>
      </article>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>会话日志加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadSessions">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="sessions" :row-key="sessionRowKey" empty-text="当前筛选条件下没有会话记录" @row-click="openSession">
        <el-table-column label="会话" min-width="240">
          <template #default="scope">
            <div class="session-identity">
              <div><el-tag :type="scope.row.identified ? 'success' : 'info'" effect="plain">{{ sessionSourceLabel(scope.row.sessionSource) }}</el-tag><strong>{{ scope.row.sessionName || '未命名会话' }}</strong></div>
              <small><code>{{ scope.row.identified ? scope.row.sessionId : scope.row.fallbackRequestId }}</code></small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="当前渠道" min-width="210">
          <template #default="scope">
            <div v-if="scope.row.currentChannel" class="primary-cell"><div class="channel-title"><strong>{{ scope.row.currentChannel.channelName }}</strong><el-tag :type="channelState(scope.row).type" effect="plain">{{ channelState(scope.row).label }}</el-tag></div><small><code>{{ scope.row.currentChannel.upstreamModel }}</code></small></div>
            <span v-else class="muted-text">未进入上游渠道</span>
          </template>
        </el-table-column>
        <el-table-column label="模型 / 调用令牌" min-width="190"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.latestModel }}</strong><small>{{ scope.row.tokenName || `令牌 #${scope.row.tokenId}` }} · <code>{{ scope.row.tokenKeyPrefix || '无历史前缀' }}</code></small></div></template></el-table-column>
        <el-table-column label="请求 / 成功率" width="130" align="right"><template #default="scope"><div class="numeric-cell"><strong class="request-count" :style="requestCountStyle(scope.row.requestCount)">{{ formatCompactNumber(scope.row.requestCount) }}</strong><small>{{ formatPercent(scope.row.successRate) }} · {{ formatCompactNumber(scope.row.attemptCount) }} 次尝试</small></div></template></el-table-column>
        <el-table-column label="Token 明细" min-width="280">
          <template #default="scope">
            <div class="session-tokens">
              <span><small>普通输入</small><strong>{{ formatCompactNumber(scope.row.normalInputTokens) }}</strong></span>
              <span><small>输出</small><strong>{{ formatCompactNumber(scope.row.outputTokens) }}</strong></span>
              <span><small>缓存读</small><strong>{{ formatCompactNumber(scope.row.cachedTokens) }}</strong></span>
              <span><small>缓存写</small><strong>{{ formatCompactNumber(scope.row.cacheWriteTokens) }}</strong></span>
              <span class="session-sent"><small>真实发送（本地分词）</small><strong>{{ formatCompactNumber(scope.row.sentTokens) }}</strong></span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="费用" width="150" align="right"><template #default="scope"><div class="numeric-cell"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template></el-table-column>
        <el-table-column label="平均性能" min-width="250"><template #default="scope"><div class="numeric-cell"><strong>首 Token {{ formatTiming(scope.row.averageFirstTokenMs, scope.row.firstTokenSampleCount) }} · 延迟 {{ formatTiming(scope.row.averageLatencyMs, scope.row.latencySampleCount) }}</strong><small>请求耗时 {{ formatTiming(scope.row.averageDurationMs, scope.row.durationSampleCount) }}</small></div></template></el-table-column>
        <el-table-column label="首次 / 最近调用" width="180"><template #default="scope"><div class="numeric-cell"><strong>{{ formatDate(scope.row.firstSeenAt) }}</strong><small>最近 {{ formatDate(scope.row.lastSeenAt) }}</small></div></template></el-table-column>
        <el-table-column label="操作" width="96" fixed="right" align="right"><template #default="scope"><div class="table-actions"><el-tooltip content="修改会话名称" placement="top"><el-button class="table-action-button" text :icon="EditPen" aria-label="修改会话名称" @click.stop="renameSession(scope.row)" /></el-tooltip><el-tooltip content="查看会话详情" placement="top"><el-button class="table-action-button" text :icon="View" aria-label="查看会话详情" @click.stop="openSession(scope.row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :disabled="loading" :total="total" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadSessions" /></footer>
    </section>

    <SessionLogDrawer v-model="drawerOpen" :summary="selectedSession" />
  </div>
</template>

<style scoped>
.session-identity { display: grid; min-width: 0; gap: 5px; }
.session-identity > div, .channel-title { display: flex; align-items: center; gap: 8px; min-width: 0; }
.session-identity strong, .channel-title strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.session-identity small { overflow: hidden; color: var(--rose-text-muted); text-overflow: ellipsis; white-space: nowrap; }
.numeric-cell { display: grid; gap: 3px; font-variant-numeric: tabular-nums; }
.numeric-cell strong { color: var(--rose-text); }
.numeric-cell small { color: var(--rose-text-muted); font-size: 11px; }
.numeric-cell .request-count { color: var(--request-count-color); }
.session-tokens { display: grid; grid-template-columns: repeat(4, minmax(52px, 1fr)); gap: 4px 9px; font-variant-numeric: tabular-nums; }
.session-tokens > span { display: grid; gap: 1px; }
.session-tokens small { color: var(--rose-text-muted); font-size: 10px; white-space: nowrap; }
.session-tokens strong { color: var(--rose-text); font-size: 12px; }
.session-sent { grid-column: 1 / -1; padding-top: 3px; border-top: 1px solid var(--rose-border); }
</style>
