<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ChatDotRound, Refresh, Search, User, View } from '@element-plus/icons-vue'
import SessionLogDrawer from '@/components/SessionLogDrawer.vue'
import type { ActiveSessionPage, CodexSessionSummary } from '@/types/gateway'
import { request } from '@/utils/api'
import { formatCompactNumber } from '@/utils/formatters'

const ACTIVE_WINDOW_MINUTES = 30

const loading = ref(true)
const errorMessage = ref('')
const sessions = ref<CodexSessionSummary[]>([])
const total = ref(0)
const searchText = ref('')
const pagination = reactive({ page: 1, pageSize: 25 })
const drawerOpen = ref(false)
const selectedSession = ref<CodexSessionSummary | null>(null)

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'short',
    timeStyle: 'medium',
    timeZone: 'Asia/Shanghai',
  }).format(new Date(value))
}

function formatRelativeActivity(value: string): string {
  const elapsedSeconds = Math.max(0, Math.floor((Date.now() - Date.parse(value)) / 1000))
  if (elapsedSeconds < 60) return '刚刚活跃'
  return `${Math.min(ACTIVE_WINDOW_MINUTES, Math.floor(elapsedSeconds / 60))} 分钟前`
}

function sessionRowKey(session: CodexSessionSummary): string {
  return `${session.tokenId}:${session.sessionId}`
}

function requestCountClass(count: number): string {
  if (count >= 100) return 'is-very-high'
  if (count >= 20) return 'is-high'
  if (count >= 5) return 'is-medium'
  return 'is-low'
}

async function loadSessions() {
  loading.value = true
  errorMessage.value = ''
  const query = new URLSearchParams({
    page: String(pagination.page),
    pageSize: String(pagination.pageSize),
  })
  if (searchText.value.trim()) query.set('session', searchText.value.trim())

  try {
    const page = await request<ActiveSessionPage>(`/admin/gateway/active-sessions?${query}`)
    sessions.value = page.items
    total.value = page.total
  } catch (error) {
    sessions.value = []
    total.value = 0
    errorMessage.value = error instanceof Error ? error.message : '活跃会话加载失败'
  } finally {
    loading.value = false
  }
}

function searchSessions() {
  pagination.page = 1
  void loadSessions()
}

function clearSearch() {
  searchText.value = ''
  pagination.page = 1
  void loadSessions()
}

function openSession(session: CodexSessionSummary) {
  selectedSession.value = session
  drawerOpen.value = true
}

onMounted(() => {
  void loadSessions()
})
</script>

<template>
  <div class="page-stack active-session-page">
    <header class="page-heading">
      <div>
        <h1>活跃会话</h1>
        <p>仅展示用户发起且最近 30 分钟内仍有调用的会话，按最近活跃时间排序</p>
      </div>
      <div class="page-actions">
        <span class="active-rule"><i aria-hidden="true" />用户会话 · 30 分钟</span>
        <el-tooltip content="刷新活跃会话" placement="bottom">
          <el-button
            class="page-refresh-button"
            :icon="Refresh"
            :loading="loading"
            aria-label="刷新活跃会话"
            @click="loadSessions"
          />
        </el-tooltip>
      </div>
    </header>

    <section class="active-query-bar" aria-label="活跃会话查询">
      <el-input
        v-model="searchText"
        clearable
        :prefix-icon="Search"
        placeholder="会话名称或会话 ID"
        aria-label="会话名称或会话 ID"
        @clear="clearSearch"
        @keyup.enter="searchSessions"
      />
      <el-button type="primary" :icon="Search" :loading="loading" @click="searchSessions">查询</el-button>
      <span class="active-count"><ChatDotRound />{{ loading ? '正在查询' : `${formatCompactNumber(total)} 个活跃会话` }}</span>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert">
      <strong>活跃会话加载失败</strong>
      <span>{{ errorMessage }}</span>
      <el-button :loading="loading" @click="loadSessions">重试</el-button>
    </div>

    <section v-else class="surface-panel table-panel active-table-panel" aria-live="polite">
      <el-table
        v-loading="loading"
        :data="sessions"
        :row-key="sessionRowKey"
        height="100%"
        empty-text="最近 30 分钟内没有活跃的用户会话"
        @row-click="openSession"
      >
        <el-table-column label="会话" min-width="320">
          <template #default="scope">
            <div class="session-cell">
              <div class="session-title-row">
                <strong :title="scope.row.sessionName || '未命名会话'">{{ scope.row.sessionName || '未命名会话' }}</strong>
                <span class="user-kind"><User />用户</span>
              </div>
              <code :title="scope.row.sessionId">{{ scope.row.sessionId }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="最近活跃" width="178">
          <template #default="scope">
            <div class="activity-cell">
              <span><i aria-hidden="true" />{{ formatRelativeActivity(scope.row.lastSeenAt) }}</span>
              <small>{{ formatDate(scope.row.lastSeenAt) }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="总请求数" width="116" align="right">
          <template #default="scope">
            <div class="request-count-cell" :class="requestCountClass(scope.row.requestCount)">
              <strong>{{ formatCompactNumber(scope.row.requestCount) }}</strong>
              <small>次</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="模型 / 接口" min-width="210">
          <template #default="scope">
            <div class="primary-cell"><strong>{{ scope.row.latestModel || '未知模型' }}</strong><small>{{ scope.row.latestEndpoint || '未知接口' }}</small></div>
          </template>
        </el-table-column>
        <el-table-column label="调用令牌" min-width="210">
          <template #default="scope">
            <div class="primary-cell">
              <strong>{{ scope.row.tokenName || `令牌 #${scope.row.tokenId}` }}</strong>
              <small><code>{{ scope.row.tokenKeyPrefix || '无历史前缀' }}</code></small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="64" fixed="right" align="right">
          <template #default="scope">
            <el-tooltip content="查看会话详情" placement="top">
              <el-button
                class="table-action-button"
                text
                :icon="View"
                aria-label="查看会话详情"
                @click.stop="openSession(scope.row)"
              />
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
      <footer class="table-pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :disabled="loading"
          :total="total"
          :page-sizes="[25, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @change="loadSessions"
        />
      </footer>
    </section>

    <SessionLogDrawer v-model="drawerOpen" :summary="selectedSession" />
  </div>
</template>

<style scoped>
.active-session-page { padding-bottom: 16px; }
.active-rule { display: inline-flex; min-height: 34px; align-items: center; gap: 7px; padding: 0 10px; border: 1px solid var(--rose-border); border-radius: var(--rose-radius-control); color: var(--rose-text-muted); background: var(--rose-surface); font-size: 12px; }
.active-rule i, .activity-cell i { width: 7px; height: 7px; flex: none; border-radius: 50%; background: var(--rose-success); }
.active-query-bar { display: grid; grid-template-columns: minmax(240px, 520px) auto minmax(140px, 1fr); align-items: center; justify-content: start; gap: 8px; padding: 12px; border: 1px solid var(--rose-border); border-radius: var(--rose-radius-panel); background: var(--rose-surface); }
.active-query-bar .el-button { min-width: 88px; }
.active-count { display: inline-flex; align-items: center; justify-content: flex-end; gap: 6px; color: var(--rose-text-muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.active-count svg { width: 15px; height: 15px; color: var(--rose-success); }
.active-table-panel { display: flex; min-width: 0; flex-direction: column; }
.active-table-panel :deep(.el-table__inner-wrapper::before) { display: none; }
.active-table-panel .table-pagination { flex: none; min-height: 56px; align-items: center; background: var(--rose-surface); }
.session-cell { display: grid; min-width: 0; gap: 6px; }
.session-title-row { display: flex; min-width: 0; align-items: center; gap: 8px; }
.session-title-row > strong { overflow: hidden; color: var(--rose-text); font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.session-cell > code { overflow: hidden; color: var(--rose-text-muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.user-kind { display: inline-flex; flex: none; align-items: center; gap: 4px; padding: 2px 6px; border: 1px solid color-mix(in srgb, var(--rose-primary) 38%, var(--rose-border)); border-radius: var(--rose-radius-control); color: var(--rose-primary-hover); background: var(--rose-primary-soft); font-size: 10px; line-height: 1.2; }
.user-kind svg { width: 11px; height: 11px; }
.activity-cell { display: grid; gap: 3px; font-variant-numeric: tabular-nums; }
.activity-cell > span { display: flex; align-items: center; gap: 6px; color: var(--rose-success); font-weight: 600; }
.activity-cell small { color: var(--rose-text-muted); font-size: 11px; }
.request-count-cell { display: inline-flex; min-width: 58px; align-items: baseline; justify-content: flex-end; gap: 3px; color: var(--rose-text-muted); font-variant-numeric: tabular-nums; }
.request-count-cell strong { font-family: var(--rose-font-mono); font-size: 13px; font-weight: 600; line-height: 1; }
.request-count-cell small { font-size: 10px; }
.request-count-cell.is-medium strong { color: var(--rose-text); font-size: 15px; font-weight: 650; }
.request-count-cell.is-high strong { color: var(--rose-primary-hover); font-size: 17px; font-weight: 700; }
.request-count-cell.is-very-high strong { color: var(--rose-primary-hover); font-size: 19px; font-weight: 750; }

@media (min-width: 961px) {
  .active-session-page { height: 100%; min-height: 0; grid-template-rows: auto auto minmax(0, 1fr); overflow: hidden; padding-bottom: 0; }
  .active-table-panel { min-height: 0; }
  .active-table-panel > .el-table { min-height: 0; flex: 1 1 0; }
}

@media (max-width: 720px) {
  .active-query-bar { grid-template-columns: minmax(0, 1fr) auto; }
  .active-count { grid-column: 1 / -1; justify-content: flex-start; }
}

@media (max-width: 460px) {
  .active-query-bar { grid-template-columns: 1fr; }
  .active-query-bar .el-button { width: 100%; }
}
</style>
