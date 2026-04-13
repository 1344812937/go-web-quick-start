<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { request } from '../utils/api'

const loading = ref(true)
const saving = ref(false)
const form = reactive({
  webConfig: {
    host: '',
    port: '',
  },
  nodeConfig: {
    sharedToken: '',
  },
  authConfig: {
    accessToken: '',
  },
})

function applySettings(payload) {
  form.webConfig.host = payload.webConfig.host || ''
  form.webConfig.port = payload.webConfig.port || ''
  form.nodeConfig.sharedToken = payload.nodeConfig.sharedToken || ''
  form.authConfig.accessToken = payload.authConfig.accessToken || ''
}

async function loadSettings() {
  loading.value = true
  try {
    const payload = await request('/settings')
    applySettings(payload)
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const payload = await request('/settings', {
      method: 'PUT',
      body: JSON.stringify(form),
    })
    applySettings(payload)
    ElMessage.success('配置已保存')
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="page-section">
    <el-card shadow="never" class="surface-card">
      <template #header>
        <div>
          <div class="section-title">程序设置</div>
          <p class="section-subtitle">直接维护 config/config.toml 对应的基础配置项。</p>
        </div>
      </template>

      <el-skeleton v-if="loading" :rows="6" animated />
      <el-form v-else label-position="top" class="form-grid">
        <el-row :gutter="18">
          <el-col :md="12" :xs="24">
            <el-form-item label="监听主机">
              <el-input v-model="form.webConfig.host" placeholder="localhost 或 0.0.0.0" />
            </el-form-item>
          </el-col>
          <el-col :md="12" :xs="24">
            <el-form-item label="监听端口">
              <el-input v-model="form.webConfig.port" placeholder="8888" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="共享令牌">
          <el-input v-model="form.nodeConfig.sharedToken" placeholder="节点互联时使用的共享令牌" />
        </el-form-item>

        <el-form-item label="访问令牌">
          <el-input v-model="form.authConfig.accessToken" placeholder="后续如启用认证，可复用此配置" />
        </el-form-item>

        <div class="inline-actions">
          <el-button type="primary" :loading="saving" @click="saveSettings">保存设置</el-button>
          <el-button @click="loadSettings">重新加载</el-button>
        </div>
      </el-form>
    </el-card>
  </div>
</template>