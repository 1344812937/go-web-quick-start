<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { request } from '../utils/api'

const loading = ref(true)
const siteInfo = ref(null)
const runningForText = ref('')

let timerId = null

const quickSteps = [
  '通过设置页修改监听地址、共享令牌和访问令牌。',
  '在 internal/api 中继续新增业务 API。',
  '在 frontend/src/pages 中继续添加业务页面并注册路由。',
]

function formatDuration(durationMs) {
  const totalSeconds = Math.max(0, Math.floor(durationMs / 1000))
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts = []

  if (days > 0) {
    parts.push(`${days}天`)
  }
  if (days > 0 || hours > 0) {
    parts.push(`${hours}时`)
  }
  if (days > 0 || hours > 0 || minutes > 0) {
    parts.push(`${minutes}分`)
  }
  parts.push(`${seconds}秒`)

  return parts.join(' ')
}

function startRunningTimer(startedAt) {
  const startedAtMs = Date.parse(startedAt)
  if (Number.isNaN(startedAtMs)) {
    runningForText.value = siteInfo.value?.runningFor || '未知'
    return
  }

  const updateRunningFor = () => {
    runningForText.value = formatDuration(Date.now() - startedAtMs)
  }

  updateRunningFor()
  timerId = window.setInterval(updateRunningFor, 1000)
}

onMounted(async () => {
  try {
    siteInfo.value = await request('/site/info')
    startRunningTimer(siteInfo.value.startedAt)
  } finally {
    loading.value = false
  }
})

onUnmounted(() => {
  if (timerId !== null) {
    window.clearInterval(timerId)
  }
})
</script>

<template>
  <div class="page-section">
    <el-card shadow="never" class="surface-card">
      <template #header>
        <div>
          <div class="section-title">运行概况</div>
          <p class="section-subtitle">这里保留了脚手架的基本识别信息与当前运行态。</p>
        </div>
      </template>

      <el-skeleton v-if="loading" :rows="4" animated />
      <div v-else class="metric-grid">
        <article class="metric-tile">
          <div class="metric-label">程序名称</div>
          <div class="metric-value">{{ siteInfo?.title || '未加载' }}</div>
          <p class="metric-description">模块名：{{ siteInfo?.appName }}</p>
        </article>
        <article class="metric-tile">
          <div class="metric-label">版本</div>
          <div class="metric-value">{{ siteInfo?.version }}</div>
          <p class="metric-description">静态资源基路径：{{ siteInfo?.basePath }}</p>
        </article>
        <article class="metric-tile">
          <div class="metric-label">监听地址</div>
          <div class="metric-value">{{ siteInfo?.host }}:{{ siteInfo?.port }}</div>
          <p class="metric-description">静态入口：{{ siteInfo?.basePath }}</p>
        </article>
        <article class="metric-tile">
          <div class="metric-label">运行时长</div>
          <div class="metric-value">{{ runningForText }}</div>
          <p class="metric-description">启动时间：{{ siteInfo?.startedAt }}</p>
        </article>
      </div>
    </el-card>

    <el-card shadow="never" class="surface-card">
      <template #header>
        <div>
          <div class="section-title">脚手架说明</div>
          <p class="section-subtitle">旧业务页面已经移除，只保留后续继续扩展所需的基本骨架。</p>
        </div>
      </template>

      <div class="list-block">
        <div class="list-item">前端保留了 Vue 3、Vue Router、Element Plus 和静态资源嵌入能力。</div>
        <div class="list-item">后端保留了配置加载、静态站点服务和统一 API 响应结构。</div>
        <div class="list-item">主页用于展示运行信息，设置页用于维护 config/config.toml。</div>
      </div>
    </el-card>

    <el-card shadow="never" class="surface-card">
      <template #header>
        <div>
          <div class="section-title">下一步扩展</div>
          <p class="section-subtitle">从这里继续长出你的业务模块，而不是继承旧工作台残留。</p>
        </div>
      </template>

      <div class="list-block">
        <div v-for="step in quickSteps" :key="step" class="list-item">
          {{ step }}
        </div>
      </div>
      <div class="inline-actions" style="margin-top: 18px;">
        <RouterLink to="/settings">
          <el-button type="primary">打开设置</el-button>
        </RouterLink>
      </div>
    </el-card>
  </div>
</template>