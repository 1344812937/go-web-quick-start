<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

const navigationItems = [
  { path: '/', label: '主页' },
  { path: '/settings', label: '设置' },
]

const pageTitle = computed(() => {
  if (route.path === '/settings') {
    return '基础设置'
  }
  return '程序主页'
})
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <div class="brand-block">
        <p class="brand-kicker">Tank Tool</p>
        <h1 class="brand-title">脚手架基础壳</h1>
        <p class="brand-summary">保留主页和设置页，后续业务模块可直接在此继续扩展。</p>
      </div>
      <nav class="nav-bar">
        <RouterLink
          v-for="item in navigationItems"
          :key="item.path"
          :to="item.path"
          class="nav-link"
          :class="{ active: route.path === item.path }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>
    </header>

    <main class="app-main">
      <section class="hero-panel">
        <div>
          <p class="hero-kicker">Scaffold</p>
          <h2 class="hero-title">{{ pageTitle }}</h2>
        </div>
        <p class="hero-description">当前仓库已收敛为最小运行骨架，包含静态资源托管、配置读写与前后端基础结构。</p>
      </section>

      <section class="content-panel">
        <slot />
      </section>
    </main>
  </div>
</template>

<style scoped>
.app-shell {
  min-height: 100vh;
  padding: 32px;
}

.app-header {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  align-items: flex-start;
  margin: 0 auto 28px;
  max-width: 1180px;
}

.brand-block {
  max-width: 540px;
}

.brand-kicker,
.hero-kicker {
  font-size: 12px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--app-text-soft);
}

.brand-title {
  font-size: clamp(32px, 5vw, 52px);
  line-height: 1;
  color: var(--app-accent-deep);
}

.brand-summary {
  margin-top: 12px;
  color: var(--app-text-soft);
}

.nav-bar {
  display: flex;
  gap: 10px;
  padding: 8px;
  border: 1px solid var(--app-line);
  border-radius: 999px;
  background: rgba(255, 249, 243, 0.78);
  box-shadow: var(--app-shadow);
}

.nav-link {
  padding: 10px 18px;
  border-radius: 999px;
  color: var(--app-text-soft);
  transition: all 0.2s ease;
}

.nav-link.active {
  color: #fffaf3;
  background: linear-gradient(135deg, var(--app-accent) 0%, var(--app-accent-deep) 100%);
}

.app-main {
  margin: 0 auto;
  max-width: 1180px;
}

.hero-panel {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: end;
  padding: 28px;
  border: 1px solid var(--app-line);
  border-radius: 32px;
  background: linear-gradient(135deg, rgba(255, 247, 239, 0.92), rgba(241, 225, 207, 0.84));
  box-shadow: var(--app-shadow);
}

.hero-title {
  margin-top: 8px;
  font-size: clamp(28px, 4vw, 44px);
  line-height: 1.05;
}

.hero-description {
  max-width: 460px;
  color: var(--app-text-soft);
}

.content-panel {
  margin-top: 24px;
}

@media (max-width: 900px) {
  .app-shell {
    padding: 18px;
  }

  .app-header,
  .hero-panel {
    flex-direction: column;
    align-items: stretch;
  }

  .nav-bar {
    width: fit-content;
  }
}
</style>