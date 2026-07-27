<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import type { Channel, ClientToken, GatewayModel, LogPage, RelayRequestLog } from '@/types/gateway'
import { request } from '@/utils/api'

const loading = ref(true)
const errorMessage = ref('')
const logs = ref<RelayRequestLog[]>([])
const total = ref(0)
const models = ref<GatewayModel[]>([])
const channels = ref<Channel[]>([])
const tokens = ref<ClientToken[]>([])
const filters = reactive({ model: '', channelId: '', tokenId: '', status: '', range: [] as Date[] })
const pagination = reactive({ page: 1, pageSize: 50 })

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('zh-CN').format(value)
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

function channelName(id: number): string {
  return channels.value.find((channel) => channel.id === id)?.name ?? `渠道 #${id}`
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

function costSource(value: RelayRequestLog['costSource']): string {
  if (value === 'upstream') return '上游返回'
  if (value === 'estimated_fallback') return '估算回退'
  if (value === 'mixed') return '混合'
  return '失败为零'
}

function costSourceType(value: RelayRequestLog['costSource']): 'success' | 'warning' | 'danger' | 'info' {
  if (value === 'upstream') return 'success'
  if (value === 'estimated_fallback' || value === 'mixed') return 'warning'
  return 'info'
}

function statusType(status: number): 'success' | 'warning' | 'danger' | 'info' {
  if (status >= 200 && status < 300) return 'success'
  if (status === 408 || status === 429) return 'warning'
  if (status >= 500 || status === 0) return 'danger'
  return 'info'
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
  if (filters.range.length === 2) {
    query.set('from', filters.range[0].toISOString())
    query.set('to', filters.range[1].toISOString())
  }
  try {
    const page = await request<LogPage>(`/admin/gateway/logs?${query}`)
    logs.value = page.items
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
  Object.assign(filters, { model: '', channelId: '', tokenId: '', status: '', range: [] })
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
      <div><h1>调用日志</h1><p>查看最近 5 天的请求状态、重试尝试、usage 来源与双费用口径</p></div>
      <div class="page-actions"><el-tooltip content="刷新调用日志" placement="bottom"><el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新调用日志" @click="loadLogs" /></el-tooltip></div>
    </header>

    <section class="filter-bar" aria-label="日志筛选">
      <el-select v-model="filters.model" clearable placeholder="全部模型"><el-option v-for="model in models" :key="model.id" :label="model.name" :value="model.name" /></el-select>
      <el-select v-model="filters.channelId" clearable placeholder="全部渠道"><el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="String(channel.id)" /></el-select>
      <el-select v-model="filters.tokenId" clearable placeholder="全部令牌"><el-option v-for="token in tokens" :key="token.id" :label="token.name" :value="String(token.id)" /></el-select>
      <el-select v-model="filters.status" clearable placeholder="全部状态"><el-option label="成功 2xx" value="200" /><el-option label="限流 429" value="429" /><el-option label="服务不可用 503" value="503" /></el-select>
      <el-date-picker v-model="filters.range" type="datetimerange" range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间" />
      <div class="filter-actions"><el-button :icon="RefreshLeft" :disabled="loading" @click="resetLogs">重置</el-button><el-button type="primary" :icon="Search" :loading="loading" @click="searchLogs">查询</el-button></div>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>调用日志加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadLogs">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="logs" row-key="id" empty-text="当前筛选条件下没有调用日志">
        <el-table-column type="expand">
          <template #default="scope">
            <div class="attempt-list">
              <header><strong>上游尝试</strong><span>共 {{ scope.row.attempts.length }} 次</span></header>
              <div v-if="scope.row.attempts.length === 0" class="muted-text">请求未进入上游调度</div>
              <div v-for="(attempt, index) in scope.row.attempts" :key="attempt.id" class="attempt-row">
                <span class="attempt-index">{{ index + 1 }}</span>
                <div><strong>{{ attempt.channelName || channelName(attempt.channelId) }}</strong><small><code>{{ attempt.upstreamModel }}</code></small></div>
                <el-tag :type="statusType(attempt.statusCode)" effect="plain">{{ attempt.statusCode || '网络错误' }}</el-tag>
                <span>{{ attempt.latencyMs }} ms</span>
                <div class="token-breakdown attempt-tokens">
                  <span><small>普通输入</small><strong>{{ formatTokens(attempt.normalInputTokens) }}</strong></span>
                  <span><small>输出</small><strong>{{ formatTokens(attempt.outputTokens) }}</strong></span>
                  <span><small>缓存读</small><strong>{{ formatTokens(attempt.cachedTokens) }}</strong></span>
                  <span><small>缓存写</small><strong>{{ formatTokens(attempt.cacheWriteTokens) }}</strong></span>
                  <span class="sent-token"><small>真实发送（本地分词）</small><strong>{{ formatTokens(attempt.sentTokens) }}</strong></span>
                </div>
                <div class="cost-breakdown"><strong>{{ formatUSD(attempt.upstreamCostMicros) }}</strong><small>估算 {{ formatUSD(attempt.estimatedCostMicros) }}</small></div>
                <div class="source-breakdown"><el-tag :type="costSourceType(attempt.costSource)" effect="plain" size="small">{{ costSource(attempt.costSource) }}</el-tag><small>{{ usageSource(attempt.usageSource) }}</small></div>
                <small v-if="attempt.errorMessage" class="attempt-error">{{ attempt.errorMessage }}</small>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="时间 / 请求 ID" min-width="220"><template #default="scope"><div class="primary-cell"><strong>{{ formatDate(scope.row.createdAt) }}</strong><small><code>{{ scope.row.id }}</code></small></div></template></el-table-column>
        <el-table-column label="会话 / 调用令牌" min-width="210"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.codexSessionId || '未识别会话' }}</strong><small>{{ tokenName(scope.row) }} · <code>{{ scope.row.tokenKeyPrefix || '无历史前缀' }}</code></small></div></template></el-table-column>
        <el-table-column label="端点 / 模型" min-width="180"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</strong><small><code>{{ scope.row.requestedModel }}</code></small></div></template></el-table-column>
        <el-table-column label="状态" width="92"><template #default="scope"><el-tag :type="statusType(scope.row.statusCode)" effect="plain">{{ scope.row.statusCode }}</el-tag></template></el-table-column>
        <el-table-column label="Token 明细" min-width="300">
          <template #default="scope">
            <div class="token-breakdown">
              <span><small>普通输入</small><strong>{{ formatTokens(scope.row.normalInputTokens) }}</strong></span>
              <span><small>输出</small><strong>{{ formatTokens(scope.row.outputTokens) }}</strong></span>
              <span><small>缓存读</small><strong>{{ formatTokens(scope.row.cachedTokens) }}</strong></span>
              <span><small>缓存写</small><strong>{{ formatTokens(scope.row.cacheWriteTokens) }}</strong></span>
              <span class="sent-token"><small>真实发送（本地分词）</small><strong>{{ formatTokens(scope.row.sentTokens) }}</strong></span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="费用" width="158" align="right"><template #default="scope"><div class="cost-breakdown"><strong>{{ formatUSD(scope.row.upstreamCostMicros) }}</strong><small>自行估算 {{ formatUSD(scope.row.estimatedCostMicros) }}</small></div></template></el-table-column>
        <el-table-column label="来源" width="142"><template #default="scope"><div class="source-breakdown"><el-tag :type="costSourceType(scope.row.costSource)" effect="plain" size="small">{{ costSource(scope.row.costSource) }}</el-tag><small>{{ usageSource(scope.row.usageSource) }}</small></div></template></el-table-column>
        <el-table-column label="耗时" width="96" align="right"><template #default="scope">{{ scope.row.durationMs }} ms</template></el-table-column>
        <el-table-column label="尝试" width="72" align="right" prop="attemptCount" />
      </el-table>
      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :disabled="loading" :total="total" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadLogs" /></footer>
    </section>
  </div>
</template>

<style scoped>
.attempt-list { display: grid; gap: 8px; padding: 14px 24px 18px 54px; background: var(--rose-surface-muted); }
.attempt-list > header { display: flex; justify-content: space-between; color: var(--rose-text-muted); font-size: 12px; }
.attempt-list > header strong { color: var(--rose-text); }
.attempt-row { display: grid; grid-template-columns: 28px minmax(140px, 1.2fr) 94px 90px minmax(300px, 1.4fr) 142px 132px; align-items: center; gap: 12px; min-width: 1060px; padding: 9px 0; border-top: 1px solid var(--rose-border); font-size: 12px; }
.attempt-row > div { display: grid; }
.token-breakdown { display: grid; grid-template-columns: repeat(4, minmax(58px, 1fr)); gap: 5px 10px; font-variant-numeric: tabular-nums; }
.token-breakdown > span { display: grid; gap: 1px; min-width: 0; }
.token-breakdown small { color: var(--rose-text-muted); font-size: 10px; white-space: nowrap; }
.token-breakdown strong { color: var(--rose-text); font-size: 12px; }
.token-breakdown .sent-token { grid-column: 1 / -1; padding-top: 3px; border-top: 1px solid var(--rose-border); }
.attempt-tokens { grid-template-columns: repeat(5, minmax(58px, 1fr)); }
.attempt-tokens .sent-token { grid-column: auto; padding-top: 0; border-top: 0; }
.attempt-index { display: grid; width: 22px; height: 22px; place-items: center; background: var(--rose-primary-soft); color: var(--rose-primary-hover); font-variant-numeric: tabular-nums; }
.attempt-error { grid-column: 2 / -1; color: var(--rose-danger); overflow-wrap: anywhere; }
.cost-breakdown, .source-breakdown { display: grid; gap: 3px; }
.cost-breakdown strong { color: var(--rose-text); font-variant-numeric: tabular-nums; }
.cost-breakdown small, .source-breakdown small { color: var(--rose-text-muted); font-size: 10px; }
.source-breakdown { justify-items: start; }
@media (max-width: 720px) { .attempt-list { padding: 10px; overflow-x: auto; } }
</style>
