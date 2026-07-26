<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Connection, Delete, Edit, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import ChannelLatencySparkline from '@/components/ChannelLatencySparkline.vue'
import type {
  Channel,
  ChannelModel,
  ChannelModelDiscovery,
  ChannelModelDiscoveryRequest,
  GatewayModel,
  UpstreamModel,
} from '@/types/gateway'
import { request } from '@/utils/api'

interface MappingDraft {
  id?: number
  modelId: number | null
  upstreamModel: string
  priority: number
  weight: number
  inputPrice: number
  outputPrice: number
  cachedInputPrice: number | null
  enabled: boolean
}

const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const channels = ref<Channel[]>([])
const models = ref<GatewayModel[]>([])
const drawerOpen = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({ name: '', baseUrl: '', apiKey: '', enabled: true, supportsStreamUsage: true })
const mappings = ref<MappingDraft[]>([])
const discoveringModels = ref(false)
const discoveryError = ref('')
const discoveredModels = ref<UpstreamModel[]>([])
const discoverySummary = ref<ChannelModelDiscovery | null>(null)
const drawerTitle = computed(() => editingId.value ? '编辑渠道' : '新增渠道')
let discoveryRequestVersion = 0

function fromMicros(value: number | null): number | null {
  return value === null ? null : value / 1_000_000
}

function mappingDraft(mapping: ChannelModel): MappingDraft {
  return {
    id: mapping.id,
    modelId: mapping.modelId,
    upstreamModel: mapping.upstreamModel,
    priority: mapping.priority,
    weight: mapping.weight,
    inputPrice: fromMicros(mapping.inputPriceMicros) ?? 0,
    outputPrice: fromMicros(mapping.outputPriceMicros) ?? 0,
    cachedInputPrice: fromMicros(mapping.cachedInputPriceMicros),
    enabled: mapping.enabled,
  }
}

function resetForm(channel?: Channel) {
  discoveryRequestVersion += 1
  editingId.value = channel?.id ?? null
  form.name = channel?.name ?? ''
  form.baseUrl = channel?.baseUrl ?? ''
  form.apiKey = ''
  form.enabled = channel?.enabled ?? true
  form.supportsStreamUsage = channel?.supportsStreamUsage ?? true
  mappings.value = channel?.models.map(mappingDraft) ?? []
  discoveringModels.value = false
  discoveryError.value = ''
  discoveredModels.value = []
  discoverySummary.value = null
  drawerOpen.value = true
  if (channel) void discoverChannelModels()
}

function addMapping() {
  if (discoveredModels.value.length === 0) {
    ElMessage.warning('当前没有可选择的上游模型')
    return
  }
  mappings.value.push({ modelId: null, upstreamModel: '', priority: 0, weight: 100, inputPrice: 0, outputPrice: 0, cachedInputPrice: null, enabled: true })
}

function modelOptionsForMapping(mapping: MappingDraft): UpstreamModel[] {
  const current = mapping.upstreamModel.trim()
  if (!current || discoveredModels.value.some((model) => model.id === current)) return discoveredModels.value
  return [{ id: current, ownedBy: '已配置', created: 0 }, ...discoveredModels.value]
}

function formatDiscoveryTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value))
}

async function discoverChannelModels(showSuccess = false) {
  if (!form.baseUrl.trim() || (!editingId.value && !form.apiKey.trim())) {
    discoveryError.value = 'Base URL 和 API key 不完整'
    return
  }
  const requestVersion = ++discoveryRequestVersion
  discoveringModels.value = true
  discoveryError.value = ''
  const payload: ChannelModelDiscoveryRequest = {
    channelId: editingId.value ?? 0,
    baseUrl: form.baseUrl.trim(),
    apiKey: form.apiKey.trim(),
  }
  try {
    const result = await request<ChannelModelDiscovery>('/admin/gateway/channels/discover-models', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
    if (requestVersion !== discoveryRequestVersion) return
    discoverySummary.value = result
    discoveredModels.value = result.models
    if (showSuccess) ElMessage.success(`已获取 ${result.models.length} 个上游模型`)
  } catch (error) {
    if (requestVersion !== discoveryRequestVersion) return
    discoverySummary.value = null
    discoveredModels.value = []
    discoveryError.value = error instanceof Error ? error.message : '上游模型获取失败'
  } finally {
    if (requestVersion === discoveryRequestVersion) discoveringModels.value = false
  }
}

function modelName(modelId: number): string {
  return models.value.find((model) => model.id === modelId)?.name ?? `#${modelId}`
}

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 }).format(value)
}

function formatTokens(value: number): string {
  return new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
}

function latencySampleLabel(channel: Channel): string {
  const visible = channel.metrics.latencySeries.length
  const total = channel.metrics.latencySampleCount
  return total > visible ? `最近 ${visible} / 5 天共 ${total} 次` : `近 5 天 ${total} 次`
}

function channelState(channel: Channel): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } {
  if (!channel.enabled) return { label: '已停用', type: 'info' }
  if (channel.circuitOpenUntil && Date.parse(channel.circuitOpenUntil) > Date.now()) return { label: '熔断中', type: 'danger' }
  if (channel.consecutiveFailures > 0) return { label: `${channel.consecutiveFailures} 次失败`, type: 'warning' }
  return { label: '可调度', type: 'success' }
}

async function loadData() {
  loading.value = true
  errorMessage.value = ''
  try {
    [channels.value, models.value] = await Promise.all([
      request<Channel[]>('/admin/gateway/channels'),
      request<GatewayModel[]>('/admin/gateway/models'),
    ])
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '渠道数据加载失败'
  } finally {
    loading.value = false
  }
}

async function saveChannel() {
  if (!form.name.trim() || !form.baseUrl.trim() || (!editingId.value && !form.apiKey.trim())) {
    ElMessage.error('请完整填写渠道名称、Base URL 和 API key')
    return
  }
  if (mappings.value.some((item) => !item.modelId || !item.upstreamModel.trim())) {
    ElMessage.error('模型映射需要选择公开模型和上游模型')
    return
  }
  saving.value = true
  try {
    const channel = await request<Channel>(editingId.value ? `/admin/gateway/channels/${editingId.value}` : '/admin/gateway/channels', {
      method: editingId.value ? 'PUT' : 'POST',
      body: JSON.stringify(form),
    })
    await request<ChannelModel[]>(`/admin/gateway/channels/${channel.id}/models`, {
      method: 'PUT',
      body: JSON.stringify(mappings.value.map((item) => ({
        modelId: item.modelId,
        upstreamModel: item.upstreamModel,
        priority: item.priority,
        weight: item.weight,
        inputPriceMicros: Math.round(item.inputPrice * 1_000_000),
        outputPriceMicros: Math.round(item.outputPrice * 1_000_000),
        cachedInputPriceMicros: item.cachedInputPrice === null ? null : Math.round(item.cachedInputPrice * 1_000_000),
        enabled: item.enabled,
      }))),
    })
    drawerOpen.value = false
    ElMessage.success('渠道配置已保存')
    await loadData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '渠道保存失败')
  } finally {
    saving.value = false
  }
}

async function testChannel(channel: Channel) {
  try {
    const result = await request<{ latencyMs: number; status: number }>(`/admin/gateway/channels/${channel.id}/test`, { method: 'POST' })
    ElMessage.success(`连接成功，HTTP ${result.status}，${result.latencyMs} ms`)
    await loadData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '连接测试失败')
    await loadData()
  }
}

async function deleteChannel(channel: Channel) {
  await ElMessageBox.confirm(`删除渠道“${channel.name}”及其全部模型映射？`, '删除渠道', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
  try {
    await request<null>(`/admin/gateway/channels/${channel.id}`, { method: 'DELETE' })
    ElMessage.success('渠道已删除')
    await loadData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '渠道删除失败')
  }
}

onMounted(loadData)
</script>

<template>
  <div class="page-stack">
    <header class="page-heading">
      <div><h1>渠道管理</h1><p>维护上游连接、模型映射与每百万 Token 价格</p></div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="loadData">刷新</el-button>
        <el-button type="primary" :icon="Plus" @click="resetForm()">新增渠道</el-button>
      </div>
    </header>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>渠道加载失败</strong><span>{{ errorMessage }}</span><el-button @click="loadData">重试</el-button></div>
    <section v-else class="surface-panel table-panel">
      <el-table v-loading="loading" :data="channels" row-key="id" empty-text="还没有渠道">
        <el-table-column label="渠道" min-width="190">
          <template #default="scope"><div class="primary-cell"><strong>{{ scope.row.name }}</strong><small>{{ scope.row.baseUrl }}</small></div></template>
        </el-table-column>
        <el-table-column label="状态" width="116"><template #default="scope"><el-tag :type="channelState(scope.row).type" effect="plain">{{ channelState(scope.row).label }}</el-tag></template></el-table-column>
        <el-table-column label="模型" min-width="160"><template #default="scope"><span v-if="scope.row.models.length">{{ scope.row.models.map((item: ChannelModel) => modelName(item.modelId)).join('、') }}</span><span v-else class="muted-text">未映射</span></template></el-table-column>
        <el-table-column label="最近延迟" min-width="280">
          <template #default="scope">
            <div v-if="scope.row.metrics.latencySeries.length" class="latency-metric-cell">
              <ChannelLatencySparkline :points="scope.row.metrics.latencySeries" :channel-name="scope.row.name" />
              <div class="metric-copy">
                <strong>{{ scope.row.metrics.latestLatencyMs }} ms</strong>
                <small>{{ latencySampleLabel(scope.row) }}</small>
                <small v-if="scope.row.latencyEwmaMs > 0">EWMA {{ Math.round(scope.row.latencyEwmaMs) }} ms</small>
              </div>
            </div>
            <span v-else class="muted-text">近 5 天无成功采样</span>
          </template>
        </el-table-column>
        <el-table-column label="缓存命中" min-width="170">
          <template #default="scope">
            <div v-if="scope.row.metrics.inputTokens > 0" class="metric-copy cache-metric">
              <strong>{{ formatPercent(scope.row.metrics.cacheHitRate) }}</strong>
              <div class="cache-meter" role="meter" aria-label="缓存命中率" aria-valuemin="0" aria-valuemax="1" :aria-valuenow="Math.min(Math.max(scope.row.metrics.cacheHitRate, 0), 1)">
                <span :style="{ width: `${Math.min(Math.max(scope.row.metrics.cacheHitRate, 0), 1) * 100}%` }" />
              </div>
              <small>{{ formatTokens(scope.row.metrics.cachedTokens) }} / {{ formatTokens(scope.row.metrics.inputTokens) }} Token</small>
            </div>
            <span v-else class="muted-text">暂无 usage 数据</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="236" fixed="right">
          <template #default="scope">
            <el-button text :icon="Connection" @click="testChannel(scope.row)">测试</el-button>
            <el-button text :icon="Edit" @click="resetForm(scope.row)">编辑</el-button>
            <el-button text type="danger" :icon="Delete" @click="deleteChannel(scope.row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!loading && channels.length === 0" class="table-empty-action"><el-button type="primary" :icon="Plus" @click="resetForm()">添加第一个渠道</el-button></div>
    </section>

    <el-drawer v-model="drawerOpen" :title="drawerTitle" size="min(720px, 100vw)" destroy-on-close>
      <el-form label-position="top" class="drawer-form">
        <div class="form-columns">
          <el-form-item label="渠道名称"><el-input v-model="form.name" placeholder="例如 OpenAI 主渠道" /></el-form-item>
          <el-form-item label="Base URL"><el-input v-model="form.baseUrl" placeholder="https://api.openai.com/v1" /></el-form-item>
        </div>
        <el-form-item :label="editingId ? '替换 API key' : 'API key'">
          <el-input v-model="form.apiKey" type="password" show-password autocomplete="new-password" :placeholder="editingId ? '留空则保持当前密钥' : '上游供应商密钥'" />
        </el-form-item>
        <div class="toggle-row"><el-checkbox v-model="form.enabled">启用渠道</el-checkbox><el-checkbox v-model="form.supportsStreamUsage">支持流式 usage 参数</el-checkbox></div>

        <div class="subsection-heading model-discovery-heading">
          <div>
            <h3>上游可用模型</h3>
            <p v-if="discoverySummary">HTTP {{ discoverySummary.status }} · {{ discoverySummary.latencyMs }} ms · {{ formatDiscoveryTime(discoverySummary.fetchedAt) }}</p>
            <p v-else>尚未获取</p>
          </div>
          <div class="model-discovery-actions">
            <el-tag v-if="discoverySummary" type="success" effect="plain">{{ discoveredModels.length }} 个模型</el-tag>
            <el-button :icon="Refresh" :loading="discoveringModels" @click="discoverChannelModels(true)">获取模型</el-button>
          </div>
        </div>
        <el-skeleton v-if="discoveringModels" :rows="3" animated />
        <div v-else-if="discoveryError" class="model-discovery-error inline-error" role="alert"><span>{{ discoveryError }}</span><el-button text @click="discoverChannelModels()">重试</el-button></div>
        <div v-else-if="discoveredModels.length === 0" class="mapping-empty">{{ discoverySummary ? '上游未返回可用模型' : '尚未获取上游模型' }}</div>
        <el-table v-else :data="discoveredModels" row-key="id" max-height="240" class="supported-model-table">
          <el-table-column label="模型 ID" min-width="200"><template #default="scope"><code>{{ scope.row.id }}</code></template></el-table-column>
          <el-table-column label="所属方" min-width="100"><template #default="scope">{{ scope.row.ownedBy || '未提供' }}</template></el-table-column>
        </el-table>

        <div class="subsection-heading"><div><h3>模型映射与价格</h3><p>价格单位为 USD / 百万 Token</p></div><el-button :icon="Plus" :disabled="discoveredModels.length === 0" @click="addMapping">添加映射</el-button></div>
        <div v-if="mappings.length === 0" class="mapping-empty">当前渠道未配置模型映射</div>
        <div v-for="(mapping, index) in mappings" :key="mapping.id ?? `new-${index}`" class="mapping-row">
          <el-select v-model="mapping.modelId" aria-label="公开模型" placeholder="公开模型"><el-option v-for="model in models" :key="model.id" :label="model.name" :value="model.id" /></el-select>
          <el-select v-model="mapping.upstreamModel" aria-label="上游模型" filterable placeholder="上游模型">
            <el-option v-for="model in modelOptionsForMapping(mapping)" :key="model.id" :label="model.id" :value="model.id">
              <div class="upstream-model-option"><span>{{ model.id }}</span><small v-if="model.ownedBy">{{ model.ownedBy }}</small></div>
            </el-option>
          </el-select>
          <el-input-number v-model="mapping.priority" :min="-1000" :max="1000" controls-position="right" aria-label="优先级" />
          <el-input-number v-model="mapping.weight" :min="1" :max="10000" controls-position="right" aria-label="权重" />
          <el-input-number v-model="mapping.inputPrice" :min="0" :precision="4" :step="0.1" controls-position="right" aria-label="输入价格" />
          <el-input-number v-model="mapping.outputPrice" :min="0" :precision="4" :step="0.1" controls-position="right" aria-label="输出价格" />
          <el-input-number v-model="mapping.cachedInputPrice" :min="0" :precision="4" :step="0.1" controls-position="right" aria-label="缓存输入价格" placeholder="同输入" />
          <el-checkbox v-model="mapping.enabled">启用</el-checkbox>
          <el-button :icon="Delete" title="删除映射" circle @click="mappings.splice(index, 1)" />
        </div>
      </el-form>
      <template #footer><el-button @click="drawerOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="saveChannel">保存渠道</el-button></template>
    </el-drawer>
  </div>
</template>

<style scoped>
.model-discovery-heading { margin-top: 0; }
.model-discovery-actions { display: flex; align-items: center; gap: 8px; }
.model-discovery-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 0; }
.supported-model-table { margin-bottom: 4px; border: 1px solid var(--rose-border); }
.upstream-model-option { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-width: 0; }
.upstream-model-option span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.upstream-model-option small { flex-shrink: 0; color: var(--rose-text-subtle); font-size: 11px; }
.latency-metric-cell { display: flex; align-items: center; gap: 12px; min-height: 48px; }
.metric-copy { display: grid; min-width: 0; gap: 2px; font-variant-numeric: tabular-nums; }
.metric-copy strong { color: var(--rose-text); font-size: 13px; font-weight: 650; }
.metric-copy small { color: var(--rose-text-muted); font-size: 11px; line-height: 1.35; white-space: nowrap; }
.cache-metric { width: 132px; }
.cache-meter { width: 100%; height: 4px; overflow: hidden; border-radius: 2px; background: var(--rose-border); }
.cache-meter span { display: block; height: 100%; background: var(--rose-amber); }
.mapping-row { display: grid; grid-template-columns: 1.1fr 1.25fr repeat(5, minmax(92px, .7fr)) auto 34px; align-items: center; gap: 8px; padding: 10px 0; border-bottom: 1px solid var(--rose-border); overflow-x: auto; }
.mapping-row > * { min-width: 92px; }
.mapping-row > :last-child, .mapping-row > :nth-last-child(2) { min-width: auto; }
.mapping-empty { padding: 24px; border: 1px dashed var(--rose-border-strong); color: var(--rose-text-muted); text-align: center; }
@media (max-width: 720px) { .model-discovery-heading { align-items: flex-start; } .model-discovery-actions { flex-wrap: wrap; justify-content: flex-end; } .mapping-row { grid-template-columns: 1fr 1fr; overflow: visible; } .mapping-row > * { width: 100%; } }
</style>
