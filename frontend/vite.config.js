import { execFileSync } from 'node:child_process'
import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'

function resolveBuildBranch() {
  const environmentBranch = (
    process.env.GATEWAY_BUILD_BRANCH
    || process.env.GITHUB_HEAD_REF
    || process.env.GITHUB_REF_NAME
    || ''
  ).trim()
  if (environmentBranch) return environmentBranch

  try {
    return execFileSync('git', ['branch', '--show-current'], { encoding: 'utf8' }).trim() || 'detached HEAD'
  } catch {
    return 'unknown'
  }
}

// https://vite.dev/config/
export default defineConfig({
  base: '/static/',
  define: {
    __BUILD_BRANCH__: JSON.stringify(resolveBuildBranch()),
  },
  plugins: [
    vue(),
    vueDevTools(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    },
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8888',
    },
  },
})
