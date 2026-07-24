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
              <el-input v-model="form.webConfig.host" placeholder="0.0.0.0 或 localhost" />
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

    <section class="project-about" aria-labelledby="project-about-title">
      <div class="project-about__heading">
        <h2 id="project-about-title">关于项目</h2>
        <span>项目说明与使用须知</span>
      </div>

      <div class="project-about__content">
        <dl class="project-about__details">
          <div>
            <dt>代码仓库</dt>
            <dd>
              <a
                href="https://github.com/1344812937/go-web-quick-start"
                target="_blank"
                rel="noopener noreferrer"
              >
                1344812937/go-web-quick-start
              </a>
            </dd>
          </div>
          <div>
            <dt>项目初衷</dt>
            <dd>
              用于快速搭建结构简单、便于修改的 Web 应用，适合功能展示、原型验证、内部工具及其他轻量用途。
            </dd>
          </div>
        </dl>

        <div class="project-about__notice">
          <h3>使用须知</h3>
          <ul>
            <li>项目按现状提供，不默认满足生产环境的安全性、稳定性或合规要求。</li>
            <li>部署前应自行配置认证、HTTPS、访问控制，并更换示例令牌或其他敏感配置。</li>
            <li>第三方依赖遵循各自许可证，使用、修改和部署风险由使用者自行评估。</li>
          </ul>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.project-about {
  display: grid;
  grid-template-columns: minmax(160px, 0.3fr) minmax(0, 1fr);
  gap: 32px;
  padding: 24px 2px 8px;
  border-top: 1px solid var(--supos-line);
  color: var(--supos-muted);
}

.project-about__heading h2 {
  color: var(--supos-muted);
  font-size: 15px;
  font-weight: 600;
}

.project-about__heading span {
  display: block;
  margin-top: 4px;
  color: var(--supos-subtle);
  font-size: 12px;
}

.project-about__content {
  min-width: 0;
}

.project-about__details {
  display: grid;
  gap: 14px;
}

.project-about__details > div {
  display: grid;
  grid-template-columns: 76px minmax(0, 1fr);
  gap: 16px;
}

.project-about__details dt,
.project-about__notice h3 {
  color: var(--supos-subtle);
  font-size: 12px;
}

.project-about__details dd {
  min-width: 0;
  color: var(--supos-muted);
}

.project-about__details a {
  color: var(--supos-muted);
  overflow-wrap: anywhere;
  text-decoration: underline;
  text-decoration-color: var(--supos-line);
  text-underline-offset: 3px;
}

.project-about__details a:hover,
.project-about__details a:focus-visible {
  color: var(--supos-blue-700);
  text-decoration-color: currentColor;
}

.project-about__details a:focus-visible {
  border-radius: var(--supos-radius-sm);
  outline: 2px solid var(--supos-blue-500);
  outline-offset: 3px;
}

.project-about__notice {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--supos-line);
}

.project-about__notice ul {
  display: grid;
  gap: 6px;
  margin-top: 8px;
  padding-left: 18px;
  color: var(--supos-muted);
}

@media (max-width: 768px) {
  .project-about {
    grid-template-columns: 1fr;
    gap: 18px;
    padding-top: 20px;
  }
}

@media (max-width: 480px) {
  .project-about__details > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
