<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh, RefreshLeft, Search, Unlock, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Channel, CircuitRecord, CircuitRecordPage, CircuitResolution } from '@/types/gateway'
import { request } from '@/utils/api'

const loading = ref(true)
const errorMessage = ref('')
const records = ref<CircuitRecord[]>([])
const channels = ref<Channel[]>([])
const total = ref(0)
const pendingManual = ref(0)
const reopeningRecordId = ref<number | null>(null)
const filters = reactive({ channelId: '', level: '', status: '' })
const pagination = reactive({ page: 1, pageSize: 50 })

const resolutionLabels: Record<CircuitResolution, string> = {
  '': '',
  automatic_recovery: '自动恢复',
  escalated: '已升级',
  manual_reopen: '人工开启',
  mapping_removed: '映射已删除',
  manual_reset: '人工重置',
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium', timeZone: 'Asia/Shanghai' }).format(new Date(value))
}

function levelLabel(level: number): string {
  return `${['', '一', '二', '三'][level] ?? level}级`
}

function levelType(level: number): 'warning' | 'danger' {
  return level >= 3 ? 'danger' : 'warning'
}

function recordStatus(record: CircuitRecord): string {
  if (record.resolvedAt) return resolutionLabels[record.resolution] || '已恢复'
  if (record.level === 3) return '待人工开启'
  if (record.openUntil && Date.parse(record.openUntil) > Date.now()) return '熔断中'
  return '恢复探测中'
}

function statusType(record: CircuitRecord): 'success' | 'warning' | 'danger' | 'info' {
  if (record.resolvedAt) return record.resolution === 'escalated' ? 'warning' : 'success'
  return record.level === 3 ? 'danger' : 'warning'
}

function canReopen(record: CircuitRecord): boolean {
  return record.level === 3 && !record.resolvedAt && record.mappingExists && record.mappingCircuitDisabled
}

function buildQuery(): URLSearchParams {
  const query = new URLSearchParams({ page: String(pagination.page), pageSize: String(pagination.pageSize) })
  if (filters.channelId) query.set('channelId', filters.channelId)
  if (filters.level) query.set('level', filters.level)
  if (filters.status) query.set('status', filters.status)
  return query
}

async function loadRecords() {
  loading.value = true
  errorMessage.value = ''
  try {
    const page = await request<CircuitRecordPage>(`/admin/gateway/circuit-records?${buildQuery()}`)
    records.value = page.items
    total.value = page.total
    pendingManual.value = page.pendingManual
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '熔断记录加载失败'
  } finally {
    loading.value = false
  }
}

function searchRecords() {
  pagination.page = 1
  void loadRecords()
}

function resetRecords() {
  Object.assign(filters, { channelId: '', level: '', status: '' })
  pagination.page = 1
  void loadRecords()
}

async function reopenMapping(record: CircuitRecord) {
  try {
    await ElMessageBox.confirm(
      `人工开启“${record.channelName} / ${record.modelName}”映射？`,
      '开启熔断映射',
      { type: 'warning', confirmButtonText: '开启映射', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  reopeningRecordId.value = record.id
  try {
    await request<null>(`/admin/gateway/circuit-records/${record.id}/reopen-mapping`, { method: 'POST' })
    ElMessage.success('映射已开启并重新参与调度')
    await loadRecords()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '映射开启失败')
  } finally {
    reopeningRecordId.value = null
  }
}

onMounted(async () => {
  try {
    channels.value = await request<Channel[]>('/admin/gateway/channels')
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '渠道筛选项加载失败'
  }
  await loadRecords()
})
</script>

<template>
  <div class="page-stack circuit-record-page">
    <header class="page-heading">
      <div><h1>熔断记录</h1><p>渠道与模型映射的分级熔断事件</p></div>
      <div class="page-actions"><el-tooltip content="刷新熔断记录" placement="bottom"><el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新熔断记录" @click="loadRecords" /></el-tooltip></div>
    </header>

    <section class="filter-bar circuit-filter-bar" aria-label="熔断记录筛选">
      <el-select v-model="filters.channelId" clearable placeholder="全部渠道"><el-option v-for="channel in channels" :key="channel.id" :label="channel.name" :value="String(channel.id)" /></el-select>
      <el-select v-model="filters.level" clearable placeholder="全部等级"><el-option label="一级熔断" value="1" /><el-option label="二级熔断" value="2" /><el-option label="三级熔断" value="3" /></el-select>
      <el-select v-model="filters.status" clearable placeholder="全部状态"><el-option label="待恢复" value="pending" /><el-option label="已结束" value="resolved" /></el-select>
      <div class="filter-actions"><el-button :icon="RefreshLeft" :disabled="loading" @click="resetRecords">重置</el-button><el-button type="primary" :icon="Search" :loading="loading" @click="searchRecords">查询</el-button></div>
    </section>

    <section v-if="!errorMessage" class="circuit-summary" aria-label="熔断记录汇总">
      <div><span><WarningFilled />当前筛选记录</span><strong>{{ total }}</strong></div>
      <div :class="{ 'has-pending': pendingManual > 0 }"><span><Unlock />待人工开启映射</span><strong>{{ pendingManual }}</strong></div>
    </section>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>熔断记录加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadRecords">重试</el-button></div>
    <section v-else class="surface-panel table-panel circuit-table-panel">
      <el-table v-loading="loading" :data="records" row-key="id" height="100%" empty-text="当前筛选条件下没有熔断记录">
        <el-table-column label="发生时间" min-width="170"><template #default="scope"><div class="primary-cell"><strong>{{ formatDate(scope.row.createdAt) }}</strong><small>记录 #{{ scope.row.id }}</small></div></template></el-table-column>
        <el-table-column label="等级 / 触发" min-width="138"><template #default="scope"><div class="primary-cell"><el-tag :type="levelType(scope.row.level)" effect="plain">{{ levelLabel(scope.row.level) }}熔断</el-tag><small>{{ scope.row.immediate ? '严重错误立即升级' : `${scope.row.failureCount} 次连续失败` }}</small></div></template></el-table-column>
        <el-table-column label="渠道" min-width="160"><template #default="scope"><div class="primary-cell"><strong>{{ scope.row.channelName }}</strong><small>渠道 #{{ scope.row.channelId }}</small></div></template></el-table-column>
        <el-table-column label="模型映射" min-width="220"><template #default="scope"><div class="primary-cell"><strong><code>{{ scope.row.modelName }}</code></strong><small><code>{{ scope.row.upstreamModel }}</code></small></div></template></el-table-column>
        <el-table-column label="状态" min-width="140"><template #default="scope"><div class="primary-cell"><el-tag :type="statusType(scope.row)" effect="plain">{{ recordStatus(scope.row) }}</el-tag><small v-if="scope.row.resolvedAt">{{ formatDate(scope.row.resolvedAt) }}</small><small v-else-if="scope.row.openUntil">截止 {{ formatDate(scope.row.openUntil) }}</small></div></template></el-table-column>
        <el-table-column label="失败原因" min-width="300"><template #default="scope"><el-tooltip :content="scope.row.message" placement="top"><span class="failure-message">{{ scope.row.message }}</span></el-tooltip></template></el-table-column>
        <el-table-column label="操作" width="70" fixed="right" align="right"><template #default="scope"><el-tooltip v-if="canReopen(scope.row)" content="人工开启模型映射" placement="top"><el-button class="table-action-button" text type="warning" :icon="Unlock" :loading="reopeningRecordId === scope.row.id" aria-label="人工开启模型映射" @click="reopenMapping(scope.row)" /></el-tooltip><span v-else class="muted-text">--</span></template></el-table-column>
      </el-table>
      <footer class="table-pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.pageSize" :disabled="loading" :total="total" :page-sizes="[25, 50, 100]" layout="total, sizes, prev, pager, next" @change="loadRecords" /></footer>
    </section>
  </div>
</template>

<style scoped>
.circuit-record-page { padding-bottom: 16px; }
.circuit-filter-bar { grid-template-columns: repeat(3, minmax(160px, 1fr)) auto; }
.circuit-summary { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border: 1px solid var(--rose-border); background: var(--rose-surface); }
.circuit-summary > div { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: 4px 16px; min-height: 66px; padding: 10px 16px; }
.circuit-summary > div + div { border-left: 1px solid var(--rose-border); }
.circuit-summary span { display: flex; align-items: center; gap: 7px; color: var(--rose-text-muted); font-size: 12px; }
.circuit-summary span svg { width: 15px; }
.circuit-summary strong { grid-row: 1 / span 2; grid-column: 2; color: var(--rose-text); font: 650 22px/1 var(--rose-font-mono); }
.circuit-summary .has-pending strong { color: var(--rose-danger); }
.circuit-table-panel { display: flex; min-width: 0; flex-direction: column; }
.circuit-table-panel :deep(.el-table__inner-wrapper::before) { display: none; }
.circuit-table-panel .table-pagination { flex: none; min-height: 56px; align-items: center; background: var(--rose-surface); }
.failure-message { display: -webkit-box; overflow: hidden; color: var(--rose-text-muted); line-height: 1.45; overflow-wrap: anywhere; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
@media (min-width: 961px) {
  .circuit-record-page { height: calc(100dvh - var(--rose-header-height) - 100px); min-height: 0; grid-template-rows: auto auto auto minmax(0, 1fr); overflow: hidden; padding-bottom: 0; }
  .circuit-table-panel { min-height: 0; }
  .circuit-table-panel > .el-table { min-height: 0; flex: 1 1 0; }
}
@media (max-width: 720px) {
  .circuit-filter-bar { grid-template-columns: 1fr; }
  .circuit-summary { grid-template-columns: 1fr; }
  .circuit-summary > div + div { border-top: 1px solid var(--rose-border); border-left: 0; }
}
</style>
