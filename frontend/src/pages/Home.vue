<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Coin, Connection, DataLine, Refresh, Tickets, Timer } from '@element-plus/icons-vue'
import type { Channel, DashboardSummary } from '@/types/gateway'
import { request } from '@/utils/api'

const router = useRouter()
const loading = ref(true)
const errorMessage = ref('')
const dashboard = ref<DashboardSummary | null>(null)
const channels = ref<Channel[]>([])

const totalTokens = computed(() => (dashboard.value?.inputTokens ?? 0) + (dashboard.value?.outputTokens ?? 0))
const availableChannels = computed(() => channels.value.filter((channel) => (
  channel.enabled && (!channel.circuitOpenUntil || Date.parse(channel.circuitOpenUntil) <= Date.now())
)).length)
const maxDailyRequests = computed(() => Math.max(1, ...(dashboard.value?.daily.map((day) => day.requests) ?? [1])))

function formatInteger(value: number): string {
  return new Intl.NumberFormat('zh-CN', { notation: value >= 100000 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value)
}

function formatUSD(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(micros / 1_000_000)
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function formatDate(value: string): string {
  const date = new Date(`${value}T00:00:00Z`)
  return `${date.getUTCMonth() + 1}/${date.getUTCDate()}`
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [summary, channelItems] = await Promise.all([
      request<DashboardSummary>('/admin/gateway/dashboard'),
      request<Channel[]>('/admin/gateway/channels'),
    ])
    dashboard.value = summary
    channels.value = channelItems
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '仪表盘加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(loadDashboard)
</script>

<template>
  <div class="page-stack dashboard-page">
    <header class="page-heading">
      <div>
        <h1>运行总览</h1>
        <p>累计令牌统计，以及最近 5 天的渠道和模型明细</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="loadDashboard">刷新</el-button>
    </header>

    <div v-if="errorMessage" class="state-panel state-error" role="alert">
      <strong>无法读取网关统计</strong>
      <span>{{ errorMessage }}</span>
      <el-button @click="loadDashboard">重试</el-button>
    </div>

    <template v-else>
      <section class="metric-strip" aria-label="网关指标">
        <article class="metric-cell">
          <span><Tickets />请求量</span>
          <strong v-if="!loading">{{ formatInteger(dashboard?.requests ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>令牌聚合统计</small>
        </article>
        <article class="metric-cell">
          <span><DataLine />成功率</span>
          <strong v-if="!loading">{{ formatPercent(dashboard?.successRate ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>最终状态为 2xx</small>
        </article>
        <article class="metric-cell">
          <span><Coin />Token</span>
          <strong v-if="!loading">{{ formatInteger(totalTokens) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>输入与输出合计</small>
        </article>
        <article class="metric-cell">
          <span><Coin />估算费用</span>
          <strong v-if="!loading">{{ formatUSD(dashboard?.estimatedCostMicros ?? 0) }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>包含已知重试用量</small>
        </article>
        <article class="metric-cell">
          <span><Timer />平均延迟</span>
          <strong v-if="!loading">{{ Math.round(dashboard?.averageLatencyMs ?? 0) }} ms</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>端到端请求耗时</small>
        </article>
        <article class="metric-cell">
          <span><Connection />可用渠道</span>
          <strong v-if="!loading">{{ availableChannels }} / {{ channels.length }}</strong>
          <el-skeleton v-else :rows="1" animated />
          <small>{{ availableChannels ? '至少一个渠道可调度' : '当前无可用渠道' }}</small>
        </article>
      </section>

      <div v-if="!loading && dashboard?.requests === 0" class="state-panel state-empty">
        <Connection />
        <strong>还没有网关调用</strong>
        <span>配置渠道、公开模型和访问令牌后，请求统计会显示在这里。</span>
        <el-button type="primary" @click="router.push('/channels')">配置渠道</el-button>
      </div>

      <template v-else>
        <section class="surface-panel usage-panel">
          <header class="panel-heading">
            <div>
              <h2>近 14 日请求</h2>
              <p>每日请求量与成功量</p>
            </div>
          </header>
          <div v-if="loading" class="chart-skeleton"><el-skeleton :rows="5" animated /></div>
          <div v-else class="bar-chart" aria-label="近 14 日请求柱状图">
            <div v-for="day in dashboard?.daily" :key="day.date" class="bar-column">
              <div class="bar-track">
                <span class="bar-total" :style="{ height: `${Math.max(3, day.requests / maxDailyRequests * 100)}%` }"></span>
                <span class="bar-success" :style="{ height: `${Math.max(0, day.successes / maxDailyRequests * 100)}%` }"></span>
              </div>
              <small>{{ formatDate(day.date) }}</small>
            </div>
          </div>
          <div class="chart-legend"><span><i class="legend-total"></i>请求</span><span><i class="legend-success"></i>成功</span></div>
        </section>

        <div class="dashboard-tables">
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>渠道分布</h2><p>最近 5 天，按尝试次数统计</p></div></header>
            <el-table :data="dashboard?.channels ?? []" empty-text="暂无渠道调用">
              <el-table-column prop="name" label="渠道" min-width="140" />
              <el-table-column prop="requests" label="尝试" width="92" align="right" />
              <el-table-column label="估算费用" width="126" align="right">
                <template #default="scope">{{ formatUSD(scope.row.estimatedCostMicros) }}</template>
              </el-table-column>
            </el-table>
          </section>
          <section class="surface-panel">
            <header class="panel-heading"><div><h2>模型分布</h2><p>最近 5 天，按公开模型统计</p></div></header>
            <el-table :data="dashboard?.models ?? []" empty-text="暂无模型调用">
              <el-table-column prop="name" label="模型" min-width="140" />
              <el-table-column prop="requests" label="请求" width="92" align="right" />
              <el-table-column label="估算费用" width="126" align="right">
                <template #default="scope">{{ formatUSD(scope.row.estimatedCostMicros) }}</template>
              </el-table-column>
            </el-table>
          </section>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.dashboard-page { min-width: 0; }
.usage-panel { min-height: 300px; }
.chart-skeleton { padding: 24px; }
.bar-chart { display: grid; grid-template-columns: repeat(14, minmax(24px, 1fr)); align-items: end; gap: 8px; height: 210px; padding: 20px 22px 12px; overflow-x: auto; }
.bar-column { display: grid; grid-template-rows: 160px 22px; align-items: end; gap: 8px; min-width: 24px; text-align: center; }
.bar-track { position: relative; height: 160px; border-bottom: 1px solid var(--rose-border); background: var(--rose-surface-muted); }
.bar-track span { position: absolute; inset: auto 0 0; min-height: 0; transition: height 180ms ease; }
.bar-total { background: var(--rose-primary-soft); }
.bar-success { left: 28% !important; right: 28% !important; background: var(--rose-success); }
.bar-column small { color: var(--rose-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.chart-legend { display: flex; justify-content: flex-end; gap: 18px; padding: 0 22px 18px; color: var(--rose-text-muted); font-size: 12px; }
.chart-legend span { display: inline-flex; align-items: center; gap: 6px; }
.chart-legend i { width: 10px; height: 10px; }
.legend-total { background: var(--rose-primary-soft); }
.legend-success { background: var(--rose-success); }
.dashboard-tables { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
@media (max-width: 860px) {
  .dashboard-tables { grid-template-columns: 1fr; }
  .bar-chart { grid-template-columns: repeat(14, minmax(30px, 1fr)); }
}
</style>
