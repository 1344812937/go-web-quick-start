<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Check, Link, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import projectMeta from '@/config/project.generated.js'
import type { ApplicationSettings, PayloadLogDetail } from '@/types/gateway'
import { request } from '@/utils/api'

const repositoryUrl = 'https://github.com/1344812937/go-web-quick-start'
const buildBranch = __BUILD_BRANCH__
const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const payloadLogDetailOptions: Array<{ label: string; value: PayloadLogDetail }> = [
  { label: '默认', value: 'default' },
  { label: '摘要', value: 'summary' },
  { label: '无', value: 'none' },
]
const payloadLogDetailDescriptions: Record<PayloadLogDetail, string> = {
  default: '沿用当前记录规则，正文按会话去重并以单段 4 MiB 为上限。',
  summary: '保留 JSON 结构、短文本预览及首尾数组项，单段不超过 64 KiB。',
  none: '不保存请求参数和响应正文，仅保留状态、用量、耗时与路由结果。',
}
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
    payloadLogDetail: 'default',
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

    <div v-if="errorMessage" class="state-panel state-error" role="alert"><strong>系统设置加载失败</strong><span>{{ errorMessage }}</span><el-button :loading="loading" @click="loadSettings">重试</el-button></div>
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
          <el-form-item label="调用日志参数 / 返回记录细节" class="payload-detail-field">
            <div class="payload-detail-control">
              <el-segmented v-model="form.gatewayConfig.payloadLogDetail" :options="payloadLogDetailOptions" aria-label="调用日志参数和返回记录细节" />
              <small>{{ payloadLogDetailDescriptions[form.gatewayConfig.payloadLogDetail] }} 保存后立即作用于新进入的调用。</small>
            </div>
          </el-form-item>
        </div>
      </section>

      <div class="settings-actions"><el-button type="primary" :icon="Check" :loading="saving" native-type="submit">保存设置</el-button></div>
    </el-form>

    <section class="surface-panel settings-section project-section">
      <header class="panel-heading"><div><h2>项目与责任说明</h2><p>当前构建来源和使用边界</p></div></header>
      <dl class="project-metadata">
        <div><dt>项目名称</dt><dd>{{ projectMeta.displayName }}</dd></div>
        <div><dt>构建分支</dt><dd><code>{{ buildBranch }}</code></dd></div>
        <div class="repository-row">
          <dt>GitHub 仓库</dt>
          <dd><el-link :icon="Link" :href="repositoryUrl" target="_blank" rel="noreferrer">{{ repositoryUrl }}</el-link></dd>
        </div>
      </dl>
      <div class="disclaimer" role="note" aria-label="免责声明">
        <strong>免责声明</strong>
        <p>本项目按“现状”提供，不附带任何明示或暗示保证。使用者应自行评估适用性、安全性与合规性，并承担部署、配置、数据处理及第三方服务调用产生的风险与责任。</p>
      </div>
    </section>
  </div>
</template>

<style scoped>
.settings-form { display: grid; gap: 16px; }
.settings-fields { padding: 20px 22px 8px; }
.settings-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 18px; }
.settings-actions { display: flex; justify-content: flex-end; position: sticky; bottom: 12px; padding: 10px; border: 1px solid var(--rose-border); background: var(--rose-surface); }
.payload-detail-field { grid-column: 1 / -1; }
.payload-detail-control { display: grid; justify-items: start; gap: 8px; }
.payload-detail-control small { color: var(--rose-text-muted); font-size: 11px; line-height: 1.6; }
.project-section { margin-top: 16px; }
.project-metadata { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; margin: 0; padding: 20px 22px; }
.project-metadata > div { min-width: 0; }
.project-metadata dt { margin-bottom: 6px; color: var(--rose-text-subtle); font-size: 11px; }
.project-metadata dd { min-width: 0; margin: 0; color: var(--rose-text); font-size: 13px; font-weight: 650; overflow-wrap: anywhere; }
.project-metadata code { font-family: var(--rose-font-mono); font-size: 12px; font-weight: 500; }
.repository-row { grid-column: 1 / -1; }
.repository-row :deep(.el-link) { max-width: 100%; font-size: 12px; vertical-align: top; }
.repository-row :deep(.el-link__inner) { min-width: 0; overflow-wrap: anywhere; text-align: left; }
.disclaimer { margin: 0 22px 22px; padding: 14px 16px; border-left: 3px solid var(--rose-warning); background: var(--rose-surface-muted); color: var(--rose-text-muted); }
.disclaimer strong { display: block; margin-bottom: 5px; color: var(--rose-text); font-size: 12px; }
.disclaimer p { margin: 0; font-size: 12px; line-height: 1.7; }
@media (max-width: 800px) { .settings-grid { grid-template-columns: 1fr 1fr; } }
@media (max-width: 520px) {
  .settings-grid, .project-metadata { grid-template-columns: 1fr; }
  .repository-row { grid-column: auto; }
  .project-metadata { gap: 14px; padding: 18px; }
  .disclaimer { margin: 0 18px 18px; }
}
</style>
