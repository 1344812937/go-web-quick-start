<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ArrowRight, DataAnalysis, Document, Monitor, Refresh, Setting } from '@element-plus/icons-vue'
import { request } from '../utils/api'

const loading = ref(true)
const siteInfo = ref(null)
const errorMessage = ref('')
const runningForText = ref('')
let timerId = null

const statusLabel = computed(() => (loading.value ? '正在读取运行状态' : errorMessage.value ? '状态读取失败' : '服务运行正常'))

function formatDuration(durationMs) {
  const totalSeconds = Math.max(0, Math.floor(durationMs / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return [hours ? `${hours}时` : '', minutes ? `${minutes}分` : '', `${seconds}秒`].filter(Boolean).join(' ') || '0秒'
}

function startRunningTimer(startedAt) {
  if (timerId !== null) {
    window.clearInterval(timerId)
    timerId = null
  }

  const startedAtMs = Date.parse(startedAt)
  if (Number.isNaN(startedAtMs)) {
    runningForText.value = siteInfo.value?.runningFor || '未知'
    return
  }
  const updateRunningFor = () => { runningForText.value = formatDuration(Date.now() - startedAtMs) }
  updateRunningFor()
  timerId = window.setInterval(updateRunningFor, 1000)
}

async function loadSiteInfo() {
  loading.value = true
  errorMessage.value = ''
  try {
    siteInfo.value = await request('/site/info')
    startRunningTimer(siteInfo.value.startedAt)
  } catch (error) {
    errorMessage.value = error.message || '无法连接到服务'
  } finally {
    loading.value = false
  }
}

onMounted(loadSiteInfo)
onUnmounted(() => { if (timerId !== null) window.clearInterval(timerId) })
</script>

<template>
  <div class="home-page">
    <section class="overview-heading">
      <div>
        <p class="eyebrow">运行总览 / 服务状态</p>
        <h1>欢迎进入工作空间</h1>
        <p class="heading-copy">这里是一个可直接扩展的 Go + Vue 工业应用起点。</p>
      </div>
      <div class="heading-actions">
        <button class="plain-action" type="button" title="刷新运行状态" @click="loadSiteInfo">
          <Refresh />
          <span>刷新状态</span>
        </button>
        <RouterLink to="/settings" class="primary-action">
          <Setting />
          <span>配置服务</span>
          <ArrowRight />
        </RouterLink>
      </div>
    </section>

    <div class="status-strip" :class="{ 'is-error': errorMessage }">
      <span class="status-dot"></span>
      <strong>{{ statusLabel }}</strong>
      <span v-if="errorMessage" class="status-message">{{ errorMessage }}</span>
      <span v-else class="status-message">静态资源、API 与配置服务已准备就绪</span>
    </div>

    <section v-if="loading" class="overview-grid" aria-label="运行指标加载中">
      <div v-for="index in 4" :key="index" class="stat-block stat-skeleton"><el-skeleton animated :rows="2" /></div>
    </section>
    <section v-else-if="errorMessage" class="error-panel" aria-live="polite">
      <DataAnalysis />
      <div>
        <strong>暂时无法获取运行指标</strong>
        <p>{{ errorMessage }}</p>
      </div>
      <button type="button" @click="loadSiteInfo">重新尝试</button>
    </section>
    <section v-else class="overview-grid" aria-label="运行指标">
      <article class="stat-block">
        <div class="stat-heading"><span>服务状态</span><Monitor /></div>
        <strong class="stat-value">在线</strong>
        <p>监听 {{ siteInfo?.host }}:{{ siteInfo?.port }}</p>
      </article>
      <article class="stat-block">
        <div class="stat-heading"><span>当前版本</span><DataAnalysis /></div>
        <strong class="stat-value mono">{{ siteInfo?.version || '-' }}</strong>
        <p>{{ siteInfo?.appName }}</p>
      </article>
      <article class="stat-block">
        <div class="stat-heading"><span>运行时长</span><Refresh /></div>
        <strong class="stat-value mono">{{ runningForText || '-' }}</strong>
        <p>自 {{ siteInfo?.startedAt }} 启动</p>
      </article>
      <article class="stat-block">
        <div class="stat-heading"><span>访问入口</span><Document /></div>
        <strong class="stat-value mono">{{ siteInfo?.basePath || '-' }}</strong>
        <p>前端静态资源路径</p>
      </article>
    </section>

    <section class="workspace-intro">
      <div class="intro-copy">
        <p class="eyebrow">快速开始</p>
        <h2>从一个清晰的工作台开始构建业务</h2>
        <p>后端保留配置加载、静态站点服务与统一 API 响应，前端通过功能菜单统一组织页面与路由。你可以从这里继续添加领域页面、数据模型和 API。</p>
      </div>
      <div class="intro-lines" aria-label="项目能力">
        <div><span class="line-index">01</span><span>配置中心</span><small>维护监听地址与访问令牌</small></div>
        <div><span class="line-index">02</span><span>接口层</span><small>在 internal/api 中继续新增业务能力</small></div>
        <div><span class="line-index">03</span><span>页面层</span><small>在 navigation/index.ts 中挂载功能菜单</small></div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.home-page { display: grid; gap: 18px; }
.overview-heading { display: flex; justify-content: space-between; align-items: flex-end; gap: 24px; padding-bottom: 5px; }
.eyebrow { color: var(--supos-blue-600); font-size: 11px; font-weight: 700; letter-spacing: 0.12em; text-transform: uppercase; }
h1 { margin-top: 8px; color: var(--supos-text); font-size: clamp(24px, 3vw, 32px); font-weight: 600; letter-spacing: 0; line-height: 1.2; }
.heading-copy { margin-top: 7px; color: var(--supos-muted); }
.heading-actions { display: flex; align-items: center; gap: 10px; }
.plain-action, .primary-action { display: inline-flex; align-items: center; gap: 7px; min-height: 34px; padding: 0 12px; border-radius: var(--supos-radius-sm); font-size: 13px; cursor: pointer; }
.plain-action { border: 1px solid var(--supos-line-strong); color: var(--supos-muted); background: var(--supos-panel); }
.plain-action:hover { border-color: var(--supos-blue-500); color: var(--supos-blue-600); }
.primary-action { border: 1px solid var(--supos-blue-600); color: #fff; background: var(--supos-blue-600); }
.primary-action:hover { background: var(--supos-blue-700); }
.plain-action :deep(svg), .primary-action :deep(svg) { width: 15px; height: 15px; }
.primary-action :deep(svg:last-child) { width: 13px; height: 13px; margin-left: 3px; }
.status-strip { display: flex; align-items: center; gap: 9px; min-height: 38px; padding: 0 12px; border: 1px solid #bde4d1; border-radius: var(--supos-radius-sm); color: #22734e; background: #f0fbf5; font-size: 12px; }
.status-strip.is-error { border-color: #f0c8c8; color: var(--supos-danger); background: #fff7f7; }
.status-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--supos-success); }
.is-error .status-dot { background: var(--supos-danger); }
.status-message { color: inherit; opacity: 0.78; }
.error-panel { display: grid; grid-template-columns: 40px 1fr auto; align-items: center; gap: 16px; min-height: 112px; padding: 20px; border: 1px solid #f0c8c8; background: #fffafa; }
.error-panel > :deep(svg) { width: 28px; height: 28px; color: var(--supos-danger); }
.error-panel strong { color: var(--supos-text); font-weight: 600; }
.error-panel p { margin-top: 5px; color: var(--supos-muted); font-size: 12px; }
.error-panel button { min-height: 32px; padding: 0 12px; border: 1px solid var(--supos-danger); border-radius: var(--supos-radius-sm); color: var(--supos-danger); background: #fff; cursor: pointer; }
.overview-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--supos-line); background: var(--supos-panel); }
.stat-block { min-height: 132px; padding: 18px 20px; border-right: 1px solid var(--supos-line); }
.stat-block:last-child { border-right: 0; }
.stat-heading { display: flex; align-items: center; justify-content: space-between; color: var(--supos-muted); font-size: 12px; }
.stat-heading :deep(svg) { width: 17px; height: 17px; color: var(--supos-blue-500); }
.stat-value { display: block; margin-top: 13px; color: var(--supos-text); font-size: 24px; font-weight: 600; line-height: 1.2; }
.stat-value.mono { font-family: var(--supos-font-mono); font-size: 20px; }
.stat-block p { margin-top: 8px; overflow: hidden; color: var(--supos-subtle); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.stat-skeleton :deep(.el-skeleton__item) { background: #edf1f5; }
.workspace-intro { display: grid; grid-template-columns: 1.05fr 1fr; gap: 40px; padding: 28px 24px; border-top: 1px solid var(--supos-line); border-bottom: 1px solid var(--supos-line); background: var(--supos-panel); }
.intro-copy h2 { margin-top: 8px; color: var(--supos-text); font-size: 21px; font-weight: 600; line-height: 1.35; }
.intro-copy > p:last-child { max-width: 620px; margin-top: 11px; color: var(--supos-muted); line-height: 1.75; }
.intro-lines { display: grid; align-content: center; }
.intro-lines > div { display: grid; grid-template-columns: 38px 100px 1fr; align-items: center; gap: 12px; min-height: 48px; border-bottom: 1px solid var(--supos-line); color: var(--supos-text); font-size: 13px; }
.intro-lines > div:last-child { border-bottom: 0; }
.line-index { color: var(--supos-blue-600); font-family: var(--supos-font-mono); font-size: 11px; }
.intro-lines small { color: var(--supos-muted); font-size: 12px; }
@media (max-width: 900px) {
  .overview-heading { align-items: flex-start; flex-direction: column; }
  .overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .stat-block:nth-child(2) { border-right: 0; }
  .stat-block:nth-child(-n + 2) { border-bottom: 1px solid var(--supos-line); }
  .workspace-intro { grid-template-columns: 1fr; gap: 20px; }
}
@media (max-width: 560px) {
  .heading-actions { width: 100%; }
  .plain-action, .primary-action { flex: 1; justify-content: center; }
  .overview-grid { grid-template-columns: 1fr; }
  .stat-block, .stat-block:nth-child(2), .stat-block:nth-child(-n + 2) { border-right: 0; border-bottom: 1px solid var(--supos-line); }
  .stat-block:last-child { border-bottom: 0; }
  .workspace-intro { padding: 22px 18px; }
  .intro-lines > div { grid-template-columns: 32px 78px 1fr; gap: 8px; }
  .error-panel { grid-template-columns: 32px 1fr; }
  .error-panel button { grid-column: 1 / -1; }
}
</style>
