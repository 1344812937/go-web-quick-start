<script setup lang="ts">
import { computed } from 'vue'
import type { RouteDecision, RouteDecisionCandidate } from '@/types/gateway'
import { formatCompactNumber, formatDuration } from '@/utils/formatters'

interface RouteDecisionPanelProps {
  /** Immutable routing calculation snapshot attached to an upstream attempt. */
  decision: RouteDecision
}

const { decision } = defineProps<RouteDecisionPanelProps>()
const candidates = computed(() => [...decision.candidates].sort((left, right) => right.probability - left.probability))

function formatPercent(value: number): string {
  return new Intl.NumberFormat('zh-CN', { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 2 }).format(value)
}

function formatExpectation(value: number): string {
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 5 }).format(value)
}

function formatCost(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 6 }).format(micros / 1_000_000)
}

function formatLatency(candidate: RouteDecisionCandidate): string {
  return candidate.latencyMs > 0 ? formatDuration(candidate.latencyMs) : '无样本'
}

function strategyLabel(): string {
  if (decision.strategy === 'lowest_cost') return '成本优先'
  if (decision.strategy === 'lowest_latency') return '效率优先'
  return '优先级加权'
}

function modeLabel(): string {
  if (decision.mode === 'session_affinity') return '会话固定，不参与随机抽取'
  if (decision.mode === 'response_affinity') return '响应关联固定，不参与随机抽取'
  return '期望度归一化后随机抽取'
}
</script>

<template>
  <section class="route-decision" aria-label="路由决策参数">
    <header>
      <div><h4>路由决策</h4><p>{{ strategyLabel() }} · {{ modeLabel() }}</p></div>
      <span>{{ candidates.length }} 个候选渠道</span>
    </header>
    <div class="decision-table-wrap">
      <table>
        <thead><tr><th>渠道 / 上游模型</th><th>优先级 × 权重</th><th>预估价格</th><th>成功率</th><th>效率</th><th>缓存率</th><th>30 分钟调用</th><th>连续调用</th><th>期望度</th><th>命中概率</th></tr></thead>
        <tbody>
          <tr v-for="candidate in candidates" :key="candidate.channelModelId" :class="{ selected: candidate.selected }">
            <td><span class="channel-name"><i aria-hidden="true" />{{ candidate.channelName }}<em v-if="candidate.selected">已命中</em></span><code>{{ candidate.upstreamModel }}</code></td>
            <td><strong>{{ candidate.priority }}</strong><span>× {{ candidate.weight }}</span></td>
            <td>{{ formatCost(candidate.expectedCostMicros) }}</td>
            <td>{{ formatPercent(candidate.successRate) }}</td>
            <td>{{ formatLatency(candidate) }}</td>
            <td>{{ formatPercent(candidate.cacheHitRate) }}</td>
            <td>{{ formatCompactNumber(candidate.recentRouteCount) }}</td>
            <td>{{ formatCompactNumber(candidate.consecutiveRoutes) }}</td>
            <td><code>{{ formatExpectation(candidate.expectation) }}</code></td>
            <td><strong class="probability">{{ formatPercent(candidate.probability) }}</strong></td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="decision-note">期望度综合优先级、配置权重、缓存折算价格、近期成功率、响应效率、缓存命中率及单位时间调用压力；连续调用会额外衰减。历史快照不会随当前渠道状态变化。</p>
  </section>
</template>

<style scoped>
.route-decision { min-width: 0; border: 1px solid var(--rose-border); background: var(--rose-surface); }
.route-decision > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 12px 14px; border-bottom: 1px solid var(--rose-border); }
.route-decision h4 { color: var(--rose-text); font-size: 13px; }
.route-decision header p, .decision-note { margin-top: 3px; color: var(--rose-text-muted); font-size: 10px; }
.route-decision header > span { flex: 0 0 auto; color: var(--rose-text-muted); font-size: 11px; }
.decision-table-wrap { overflow-x: auto; }
table { width: 100%; min-width: 1050px; border-collapse: collapse; font-size: 11px; font-variant-numeric: tabular-nums; }
th { padding: 8px 10px; color: var(--rose-text-subtle); background: var(--rose-surface-muted); font-size: 9px; font-weight: 650; text-align: left; white-space: nowrap; }
td { padding: 9px 10px; border-top: 1px solid var(--rose-border); color: var(--rose-text-muted); white-space: nowrap; }
tbody tr:first-child td { border-top: 0; }
tbody tr.selected { background: var(--rose-primary-soft); }
td:first-child { display: grid; gap: 3px; min-width: 180px; white-space: normal; }
.channel-name { display: flex; align-items: center; gap: 6px; color: var(--rose-text); font-weight: 650; }
.channel-name i { width: 6px; height: 6px; border-radius: 50%; background: var(--rose-border-strong); }
.selected .channel-name i { background: var(--rose-primary); }
.channel-name em { padding: 1px 5px; border: 1px solid var(--rose-primary); color: var(--rose-primary-hover); font-size: 8px; font-style: normal; font-weight: 600; }
td:nth-child(2) { display: table-cell; }
td:nth-child(2) strong { margin-right: 3px; color: var(--rose-text); }
td code { color: var(--rose-text-muted); overflow-wrap: anywhere; }
.probability { color: var(--rose-primary-hover); font-size: 12px; }
.decision-note { margin: 0; padding: 9px 14px; border-top: 1px solid var(--rose-border); line-height: 1.5; }
@media (max-width: 560px) { .route-decision > header { flex-direction: column; gap: 5px; } }
</style>
