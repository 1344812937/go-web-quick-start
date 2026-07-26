<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { ApplicationSettings } from '@/types/gateway'
import { request } from '@/utils/api'

const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const form = reactive<ApplicationSettings>({
  webConfig: { host: '', port: '' },
  nodeConfig: { sharedToken: '' },
  gatewayConfig: {
    maxAttempts: 3,
    requestBodyLimitMB: 32,
    responseHeaderTimeoutSeconds: 120,
    streamIdleTimeoutSeconds: 300,
    sessionTTLHours: 12,
    secureCookie: false,
  },
})

function applySettings(payload: ApplicationSettings) {
  form.webConfig = { ...payload.webConfig }
  form.nodeConfig = { ...payload.nodeConfig }
  form.gatewayConfig = { ...payload.gatewayConfig }
}

async function loadSettings() {
  loading.value = true
  errorMessage.value = ''
  try {
    applySettings(await request<ApplicationSettings>('/settings'))
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '系统设置加载失败'
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    applySettings(await request<ApplicationSettings>('/settings', { method: 'PUT', body: JSON.stringify(form) }))
    ElMessage.success('系统设置已保存')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '系统设置保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="page-stack settings-page">
    <header class="page-heading">
      <div><h1>系统设置</h1><p>调整监听地址、请求限制、超时和管理会话</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="loadSettings">重新加载</el-button>
    </header>

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>系统设置加载失败</strong><span>{{ errorMessage }}</span><el-button @click="loadSettings">重试</el-button></div>
    <el-skeleton v-else-if="loading" :rows="10" animated />
    <el-form v-else label-position="top" class="settings-form" @submit.prevent="saveSettings">
      <section class="surface-panel settings-section">
        <header class="panel-heading"><div><h2>服务监听</h2><p>监听地址变更后需重启进程</p></div></header>
        <div class="settings-fields form-columns">
          <el-form-item label="监听主机"><el-input v-model="form.webConfig.host" placeholder="0.0.0.0" /></el-form-item>
          <el-form-item label="监听端口"><el-input v-model="form.webConfig.port" placeholder="8888" /></el-form-item>
        </div>
      </section>

      <section class="surface-panel settings-section">
        <header class="panel-heading"><div><h2>请求与重试</h2><p>作用于公开的 /v1 Chat Completions 与 Responses 接口</p></div></header>
        <div class="settings-fields settings-grid">
          <el-form-item label="单请求最大尝试次数"><el-input-number v-model="form.gatewayConfig.maxAttempts" :min="1" :max="3" controls-position="right" /></el-form-item>
          <el-form-item label="请求体上限（MiB）"><el-input-number v-model="form.gatewayConfig.requestBodyLimitMB" :min="1" :max="256" controls-position="right" /></el-form-item>
          <el-form-item label="响应头等待（秒）"><el-input-number v-model="form.gatewayConfig.responseHeaderTimeoutSeconds" :min="1" :max="600" controls-position="right" /></el-form-item>
          <el-form-item label="流式空闲超时（秒）"><el-input-number v-model="form.gatewayConfig.streamIdleTimeoutSeconds" :min="10" :max="3600" controls-position="right" /></el-form-item>
        </div>
      </section>

      <section class="surface-panel settings-section">
        <header class="panel-heading"><div><h2>会话与数据</h2><p>调用明细固定保留 5 天，更早数据仅保留按令牌汇总的每日统计</p></div></header>
        <div class="settings-fields settings-grid">
          <el-form-item label="管理会话时长（小时）"><el-input-number v-model="form.gatewayConfig.sessionTTLHours" :min="1" :max="168" controls-position="right" /></el-form-item>
          <el-form-item label="Cookie 安全属性"><el-switch v-model="form.gatewayConfig.secureCookie" active-text="仅 HTTPS 发送" inactive-text="允许本地 HTTP" /></el-form-item>
        </div>
      </section>

      <div class="settings-actions"><el-button type="primary" :icon="Check" :loading="saving" native-type="submit">保存设置</el-button></div>
    </el-form>
  </div>
</template>

<style scoped>
.settings-form { display: grid; gap: 16px; }
.settings-fields { padding: 20px 22px 8px; }
.settings-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 18px; }
.settings-actions { display: flex; justify-content: flex-end; position: sticky; bottom: 12px; padding: 10px; border: 1px solid var(--rose-border); background: var(--rose-surface); }
@media (max-width: 800px) { .settings-grid { grid-template-columns: 1fr 1fr; } }
@media (max-width: 520px) { .settings-grid { grid-template-columns: 1fr; } }
</style>
