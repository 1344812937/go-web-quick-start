<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, Search, View } from '@element-plus/icons-vue'
import SessionLogDrawer from '@/components/SessionLogDrawer.vue'
import type { Channel, ClientToken, CodexSessionPage, CodexSessionSummary, GatewayModel } from '@/types/gateway'
import { request } from '@/utils/api'

const loading = ref(true)
const errorMessage = ref('')
const sessions = ref<CodexSessionSummary[]>([])
const total = ref(0)
const models = ref<GatewayModel[]>([])
const channels = ref<Channel[]>([])
const tokens = ref<ClientToken[]>([])
const filters = reactive({ session: '', model: '', channelId: '', tokenId: '', range: [] as Date[] })
const pagination = reactive({ page: 1, pageSize: 25 })
const drawerOpen = ref(false)
const selectedSession = ref<CodexSessionSummary | null>(null)

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('zh-CN').format(value)
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
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
  if (filters.range.length === 2) {
    query.set('from', filters.range[0].toISOString())
    query.set('to', filters.range[1].toISOString())
  }
  try {
    const page = await request<CodexSessionPage>(`/admin/gateway/sessions?${query}`)
    sessions.value = page.items
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

function openSession(session: CodexSessionSummary) {
  selectedSession.value = session
  drawerOpen.value = true
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
      <div><h1>会话日志</h1><p>按 Codex 客户端会话汇总最近 5 天的渠道、模型、令牌与用量</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="loadSessions">刷新</el-button>
    </header>

    <section class="filter-bar" aria-label="会话日志筛选">
      <el-input v-model="filters.session" clearable placeholder="会话 ID 或请求 ID" @keyup.enter="searchSessions" />
      <el-select v-model="filters.model" clearable placeholder="全部模型"><el-option v-for="model in models" :key="model.id" :label="model.name" :value="model.name" /></el-select>
      <el-select v-model="filters.channelId" clearable placeholder="全部渠道"><el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="String(channel.id)" /></el-select>
      <el-select v-model="filters.tokenId" clearable placeholder="全部令牌"><el-option v-for="token in tokens" :key="token.id" :label="token.name" :value="String(token.id)" /></el-select>
      <el-date-picker v-model="filters.range" type="datetimerange" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
      <el-button type="primary" :icon="Search" @click="searchSessions">查询</el-button>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>会话日志加载失败</strong><span>{{ errorMessage }}</span><el-button @click="loadSessions">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="sessions" :row-key="sessionRowKey" empty-text="当前筛选条件下没有会话记录" @row-click="openSession">
        <el-table-column label="Codex 会话" min-width="240">
          <template #default="scope">
            <div class="session-identity">
              <div><el-tag :type="scope.row.identified ? 'success' : 'info'" effect="plain">{{ sessionSourceLabel(scope.row.sessionSource) }}</el-tag><strong>{{ scope.row.identified ? scope.row.sessionId : '未识别会话' }}</strong></div>
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
        <el-table-column label="请求 / 成功率" width="130" align="right"><template #default="scope"><div class="numeric-cell"><strong>{{ scope.row.requestCount }}</strong><small>{{ formatPercent(scope.row.successRate) }} · {{ scope.row.attemptCount }} 次尝试</small></div></template></el-table-column>
        <el-table-column label="Token（入 / 出 / 缓存）" min-width="190" align="right"><template #default="scope"><div class="numeric-cell"><strong>{{ formatTokens(scope.row.inputTokens) }} / {{ formatTokens(scope.row.outputTokens) }} / {{ formatTokens(scope.row.cachedTokens) }}</strong><small>缓存命中 {{ formatPercent(scope.row.cacheHitRate) }}</small></div></template></el-table-column>
        <el-table-column label="费用 / 平均耗时" width="148" align="right"><template #default="scope"><div class="numeric-cell"><strong>{{ formatUSD(scope.row.estimatedCostMicros) }}</strong><small>{{ Math.round(scope.row.averageDurationMs) }} ms</small></div></template></el-table-column>
        <el-table-column label="最近调用" width="168"><template #default="scope">{{ formatDate(scope.row.lastSeenAt) }}</template></el-table-column>
        <el-table-column label="详情" width="70" fixed="right"><template #default="scope"><el-button text :icon="View" title="查看会话详情" @click.stop="openSession(scope.row)" /></template></el-table-column>
      </el-table>
      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :total="total" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadSessions" /></footer>
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
</style>
