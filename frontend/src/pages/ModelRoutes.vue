<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Delete, Edit, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Channel, ChannelModel, GatewayModel, RoutingStrategy } from '@/types/gateway'
import { request } from '@/utils/api'

interface CandidateRow {
  channel: Channel
  mapping: ChannelModel
}

interface CandidateState {
  label: string
  type: 'success' | 'warning' | 'info'
}

const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const models = ref<GatewayModel[]>([])
const channels = ref<Channel[]>([])
const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const deletingModelId = ref<number | null>(null)
const form = reactive<{ name: string; routingStrategy: RoutingStrategy; enabled: boolean }>({ name: '', routingStrategy: 'priority_weighted', enabled: true })
const dialogTitle = computed(() => editingId.value ? '编辑公开模型' : '新增公开模型')
const strategies: Array<{ value: RoutingStrategy; label: string; note: string }> = [
  { value: 'priority_weighted', label: '优先级加权', note: '优先级高的渠道先选，同级按权重分配' },
  { value: 'lowest_cost', label: '最低成本', note: '按本次输入和预计输出 Token 选择' },
  { value: 'lowest_latency', label: '最低延迟', note: '按成功请求延迟 EWMA 选择并探测新渠道' },
]

function strategyLabel(value: RoutingStrategy): string {
  return strategies.find((item) => item.value === value)?.label ?? value
}

function candidates(modelId: number): CandidateRow[] {
  const rows = channels.value.flatMap((channel) => channel.models
    .filter((mapping) => mapping.modelId === modelId)
    .map((mapping) => ({ channel, mapping })))
  return rows.sort((left, right) => {
    if (left.mapping.enabled !== right.mapping.enabled) return left.mapping.enabled ? -1 : 1
    const leftChannelAvailable = left.channel.enabled && !isCircuitOpen(left.channel)
    const rightChannelAvailable = right.channel.enabled && !isCircuitOpen(right.channel)
    if (leftChannelAvailable !== rightChannelAvailable) return leftChannelAvailable ? -1 : 1
    if (left.mapping.priority !== right.mapping.priority) return right.mapping.priority - left.mapping.priority
    return left.channel.name.localeCompare(right.channel.name, 'zh-CN')
  })
}

function isCircuitOpen(channel: Channel): boolean {
  return channel.circuitOpenUntil !== null && Date.parse(channel.circuitOpenUntil) > Date.now()
}

function channelState(channel: Channel): CandidateState {
  if (!channel.enabled) return { label: '渠道停用', type: 'info' }
  if (isCircuitOpen(channel)) return { label: '熔断中', type: 'warning' }
  return { label: '可用', type: 'success' }
}

function routableCandidateCount(modelId: number): number {
  return candidates(modelId).filter(({ channel, mapping }) => mapping.enabled && channel.enabled && !isCircuitOpen(channel)).length
}

function candidateSummary(modelId: number): string {
  const rows = candidates(modelId)
  if (rows.length === 0) return '无渠道映射'
  return `${routableCandidateCount(modelId)} 个可路由 / ${rows.length} 个映射`
}

function formatPrice(micros: number | null): string {
  if (micros === null) return '同输入价'
  return `$${(micros / 1_000_000).toFixed(4)}`
}

function formatMultiplier(basisPoints: number): string {
  return `${(Number.isFinite(basisPoints) ? basisPoints / 10_000 : 1).toFixed(2)}x`
}

function openEditor(model?: GatewayModel) {
  editingId.value = model?.id ?? null
  form.name = model?.name ?? ''
  form.routingStrategy = model?.routingStrategy ?? 'priority_weighted'
  form.enabled = model?.enabled ?? true
  dialogOpen.value = true
}

async function loadData() {
  loading.value = true
  errorMessage.value = ''
  try {
    [models.value, channels.value] = await Promise.all([
      request<GatewayModel[]>('/admin/gateway/models'),
      request<Channel[]>('/admin/gateway/channels'),
    ])
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '模型路由加载失败'
  } finally {
    loading.value = false
  }
}

async function saveModel() {
  if (!form.name.trim()) {
    ElMessage.error('请输入公开模型名称')
    return
  }
  saving.value = true
  try {
    await request<GatewayModel>(editingId.value ? `/admin/gateway/models/${editingId.value}` : '/admin/gateway/models', {
      method: editingId.value ? 'PUT' : 'POST',
      body: JSON.stringify(form),
    })
    dialogOpen.value = false
    ElMessage.success('公开模型已保存')
    await loadData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '模型保存失败')
  } finally {
    saving.value = false
  }
}

async function deleteModel(model: GatewayModel) {
  await ElMessageBox.confirm(`删除公开模型“${model.name}”及相关渠道映射？`, '删除模型', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
  deletingModelId.value = model.id
  try {
    await request<null>(`/admin/gateway/models/${model.id}`, { method: 'DELETE' })
    ElMessage.success('公开模型已删除')
    await loadData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '模型删除失败')
  } finally {
    deletingModelId.value = null
  }
}

onMounted(loadData)
</script>

<template>
  <div class="page-stack">
    <header class="page-heading">
      <div><h1>模型路由</h1><p>管理公开模型名称、候选渠道矩阵和动态调度策略</p></div>
      <div class="page-actions"><el-tooltip content="刷新模型列表" placement="bottom"><el-button class="page-refresh-button" :icon="Refresh" :loading="loading" aria-label="刷新模型列表" @click="loadData" /></el-tooltip><el-button type="primary" :icon="Plus" @click="openEditor()">新增模型</el-button></div>
    </header>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>模型路由加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadData">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="models" row-key="id" empty-text="还没有公开模型">
        <el-table-column type="expand">
          <template #default="scope">
            <div class="candidate-matrix">
              <div class="matrix-heading"><strong>候选渠道</strong><span>{{ routableCandidateCount(scope.row.id) }} 个可路由 / {{ candidates(scope.row.id).length }} 个映射</span></div>
              <el-table :data="candidates(scope.row.id)" empty-text="请在渠道管理中添加模型映射">
                <el-table-column label="渠道" min-width="140"><template #default="candidate">{{ candidate.row.channel.name }}</template></el-table-column>
                <el-table-column label="映射状态" width="104"><template #default="candidate"><el-tag :type="candidate.row.mapping.enabled ? 'success' : 'info'" effect="plain" size="small">{{ candidate.row.mapping.enabled ? '已启用' : '已停用' }}</el-tag></template></el-table-column>
                <el-table-column label="渠道状态" width="104"><template #default="candidate"><el-tag :type="channelState(candidate.row.channel).type" effect="plain" size="small">{{ channelState(candidate.row.channel).label }}</el-tag></template></el-table-column>
                <el-table-column label="上游模型" min-width="160"><template #default="candidate"><code>{{ candidate.row.mapping.upstreamModel }}</code></template></el-table-column>
                <el-table-column label="优先级 / 权重" width="132" align="right"><template #default="candidate">{{ candidate.row.mapping.priority }} / {{ candidate.row.mapping.weight }}</template></el-table-column>
                <el-table-column label="价格倍率" width="96" align="right"><template #default="candidate"><code>{{ formatMultiplier(candidate.row.mapping.priceMultiplierBasisPoints) }}</code></template></el-table-column>
                <el-table-column label="输入" width="104" align="right"><template #default="candidate">{{ formatPrice(candidate.row.mapping.inputPriceMicros) }}</template></el-table-column>
                <el-table-column label="输出" width="104" align="right"><template #default="candidate">{{ formatPrice(candidate.row.mapping.outputPriceMicros) }}</template></el-table-column>
                <el-table-column label="缓存读" width="104" align="right"><template #default="candidate">{{ formatPrice(candidate.row.mapping.cachedInputPriceMicros) }}</template></el-table-column>
                <el-table-column label="缓存写" width="104" align="right"><template #default="candidate">{{ formatPrice(candidate.row.mapping.cacheWritePriceMicros) }}</template></el-table-column>
                <el-table-column label="延迟" width="96" align="right"><template #default="candidate">{{ candidate.row.channel.latencyEwmaMs ? `${Math.round(candidate.row.channel.latencyEwmaMs)} ms` : '待采样' }}</template></el-table-column>
              </el-table>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="公开模型" min-width="210"><template #default="scope"><div class="primary-cell"><strong><code>{{ scope.row.name }}</code></strong><small>{{ candidateSummary(scope.row.id) }}</small></div></template></el-table-column>
        <el-table-column label="调度策略" min-width="150"><template #default="scope">{{ strategyLabel(scope.row.routingStrategy) }}</template></el-table-column>
        <el-table-column label="状态" width="106"><template #default="scope"><el-tag :type="scope.row.enabled ? 'success' : 'info'" effect="plain">{{ scope.row.enabled ? '已公开' : '已停用' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="92" fixed="right" align="right"><template #default="scope"><div class="table-actions"><el-tooltip content="编辑公开模型" placement="top"><el-button class="table-action-button" text :icon="Edit" :disabled="deletingModelId === scope.row.id" aria-label="编辑公开模型" @click="openEditor(scope.row)" /></el-tooltip><el-tooltip content="删除公开模型" placement="top"><el-button class="table-action-button" text type="danger" :icon="Delete" :loading="deletingModelId === scope.row.id" aria-label="删除公开模型" @click="deleteModel(scope.row)" /></el-tooltip></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && models.length === 0" class="table-empty-action"><el-button type="primary" :icon="Plus" @click="openEditor()">添加第一个公开模型</el-button></div>
    </section>

    <el-dialog v-model="dialogOpen" :title="dialogTitle" width="min(520px, calc(100vw - 32px))">
      <el-form label-position="top" @submit.prevent="saveModel">
        <el-form-item label="公开模型名称"><el-input v-model="form.name" placeholder="例如 gpt-4.1" /></el-form-item>
        <el-form-item label="调度策略">
          <el-select v-model="form.routingStrategy" class="full-width"><el-option v-for="strategy in strategies" :key="strategy.value" :label="strategy.label" :value="strategy.value"><div class="select-option"><strong>{{ strategy.label }}</strong><small>{{ strategy.note }}</small></div></el-option></el-select>
        </el-form-item>
        <el-checkbox v-model="form.enabled">公开并允许调用</el-checkbox>
      </el-form>
      <template #footer><div class="dialog-actions"><el-button @click="dialogOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="saveModel">保存模型</el-button></div></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.candidate-matrix { padding: 12px 24px 20px 54px; background: var(--rose-surface-muted); }
.matrix-heading { display: flex; justify-content: space-between; align-items: center; padding: 0 0 10px; color: var(--rose-text-muted); font-size: 12px; }
.matrix-heading strong { color: var(--rose-text); }
.select-option { display: grid; line-height: 1.3; }
.select-option small { color: var(--rose-text-muted); font-size: 11px; }
@media (max-width: 640px) { .candidate-matrix { padding: 10px; } }
</style>
