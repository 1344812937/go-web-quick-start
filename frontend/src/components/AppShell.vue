<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  Bell,
  Expand,
  Fold,
  Menu,
  QuestionFilled,
  Search,
  UserFilled,
} from '@element-plus/icons-vue'
import SidebarMenuItem from '@/components/SidebarMenuItem.vue'
import projectMeta from '@/config/project.generated.js'
import { navigationGroups } from '@/navigation'

const route = useRoute()
const sidebarCollapsed = ref(false)
const mobileOpen = ref(false)
const isMobileViewport = ref(false)
const searchQuery = ref('')
let mobileMediaQuery

function filterNavigationItems(items, query) {
  return items.flatMap((item) => {
    if (item.label.toLowerCase().includes(query)) return [{ ...item }]

    const children = item.children?.length
      ? filterNavigationItems(item.children, query)
      : []
    return children.length ? [{ ...item, children }] : []
  })
}

const filteredGroups = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return navigationGroups

  return navigationGroups
    .map((group) => ({
      ...group,
      items: filterNavigationItems(group.items, query),
    }))
    .filter((group) => group.items.length > 0)
})

function collectOpenMenuKeys(items, path, openAll, keys) {
  let containsActivePath = false
  items.forEach((item) => {
    const childContainsActivePath = item.children?.length
      ? collectOpenMenuKeys(item.children, path, openAll, keys)
      : false
    const isActivePath = item.path === path
    if (item.children?.length && (openAll || childContainsActivePath)) {
      keys.add(item.key)
    }
    containsActivePath ||= isActivePath || childContainsActivePath
  })
  return containsActivePath
}

const defaultOpenMenuKeys = computed(() => {
  const keys = new Set()
  const openAll = searchQuery.value.trim().length > 0
  filteredGroups.value.forEach((group) => {
    collectOpenMenuKeys(group.items, route.path, openAll, keys)
  })
  return [...keys]
})

const menuCollapsed = computed(() => sidebarCollapsed.value && !isMobileViewport.value)
const breadcrumbs = computed(() => route.meta.breadcrumbs ?? ['工作空间'])

function syncMobileViewport(event) {
  isMobileViewport.value = event.matches
}

onMounted(() => {
  mobileMediaQuery = window.matchMedia('(max-width: 960px)')
  syncMobileViewport(mobileMediaQuery)
  if (mobileMediaQuery.addEventListener) {
    mobileMediaQuery.addEventListener('change', syncMobileViewport)
  } else {
    mobileMediaQuery.addListener(syncMobileViewport)
  }
})

onBeforeUnmount(() => {
  if (!mobileMediaQuery) return
  if (mobileMediaQuery.removeEventListener) {
    mobileMediaQuery.removeEventListener('change', syncMobileViewport)
  } else {
    mobileMediaQuery.removeListener(syncMobileViewport)
  }
})

watch(() => route.path, () => {
  mobileOpen.value = false
})

function toggleSidebar() {
  if (isMobileViewport.value) {
    mobileOpen.value = !mobileOpen.value
    return
  }
  sidebarCollapsed.value = !sidebarCollapsed.value
}
</script>

<template>
  <div class="app-shell" :class="{ 'is-sidebar-collapsed': sidebarCollapsed, 'is-mobile-open': mobileOpen }">
    <header class="app-header">
      <div class="header-brand">
        <button class="icon-button header-menu-button" type="button" title="切换导航" @click="toggleSidebar">
          <Menu />
        </button>
        <div class="brand-mark" aria-hidden="true">{{ projectMeta.appName.slice(0, 1).toUpperCase() }}</div>
        <div class="header-brand-copy">
          <strong>{{ projectMeta.displayName }}</strong>
          <span>工业应用工作台</span>
        </div>
        <span class="header-divider" aria-hidden="true"></span>
        <span class="tenant-name">默认工作空间</span>
      </div>

      <div class="header-actions">
        <span class="header-status"><i></i>服务运行中</span>
        <span class="header-version">v{{ projectMeta.version }}</span>
        <button class="icon-button" type="button" title="帮助"><QuestionFilled /></button>
        <button class="icon-button" type="button" title="通知"><Bell /></button>
        <button class="user-button" type="button" title="当前用户">
          <UserFilled />
          <span>管理员</span>
        </button>
      </div>
    </header>

    <aside class="app-sidebar">
      <div class="sidebar-toolbar">
        <div class="sidebar-search">
          <Search />
          <input v-model="searchQuery" aria-label="搜索菜单" placeholder="搜索菜单" />
        </div>
        <button class="icon-button sidebar-toggle" type="button" :title="sidebarCollapsed ? '展开导航' : '收起导航'" @click="toggleSidebar">
          <Expand v-if="sidebarCollapsed" />
          <Fold v-else />
        </button>
      </div>

      <nav class="sidebar-nav" aria-label="主导航">
        <div v-if="filteredGroups.length === 0" class="sidebar-empty">未找到菜单</div>
        <section v-for="group in filteredGroups" :key="group.label" class="nav-group">
          <div class="nav-group-label">{{ group.label }}</div>
          <el-menu
            :key="`${group.label}:${searchQuery}:${route.path}`"
            class="sidebar-menu"
            :default-active="route.path"
            :default-openeds="defaultOpenMenuKeys"
            :collapse="menuCollapsed"
            :collapse-transition="false"
            router
          >
            <SidebarMenuItem
              v-for="item in group.items"
              :key="item.key"
              :item="item"
            />
          </el-menu>
        </section>
      </nav>

      <div class="sidebar-footer">
        <div class="connection-indicator"><i></i><span>本地服务</span></div>
        <span class="sidebar-footer-version">{{ projectMeta.appName }}</span>
      </div>
    </aside>

    <main class="app-main">
      <div class="workspace-toolbar">
        <div class="breadcrumbs">
          <template v-for="(item, index) in breadcrumbs" :key="`${item}-${index}`">
            <b v-if="index > 0">/</b>
            <strong v-if="index === breadcrumbs.length - 1">{{ item }}</strong>
            <span v-else>{{ item }}</span>
          </template>
        </div>
        <div class="workspace-tools">
          <span class="workspace-hint">{{ projectMeta.description }}</span>
        </div>
      </div>
      <section class="workspace-content">
        <slot />
      </section>
    </main>

    <button v-if="mobileOpen" class="mobile-scrim" type="button" aria-label="关闭导航" @click="mobileOpen = false"></button>
  </div>
</template>

<style scoped>
.app-shell {
  min-height: 100vh;
  background: var(--supos-page);
}

.app-header {
  position: fixed;
  inset: 0 0 auto;
  z-index: 30;
  height: var(--supos-header-height);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 0 24px;
  color: #fff;
  background: var(--supos-blue-600);
  box-shadow: 0 1px 0 rgba(0, 47, 105, 0.24);
}

.header-brand,
.header-actions,
.user-button,
.workspace-tools,
.breadcrumbs,
.sidebar-toolbar,
.connection-indicator {
  display: flex;
  align-items: center;
}

.header-brand { min-width: 0; gap: 12px; }
.header-actions { gap: 12px; flex-shrink: 0; }
.header-menu-button { display: none !important; }

.brand-mark {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.44);
  border-radius: var(--supos-radius-sm);
  color: #fff;
  font-weight: 700;
  letter-spacing: 0;
}

.header-brand-copy { display: grid; gap: 1px; min-width: 0; }
.header-brand-copy strong { font-size: 15px; line-height: 1.2; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.header-brand-copy span, .tenant-name, .header-version { color: rgba(255, 255, 255, 0.72); font-size: 12px; }
.header-divider { width: 1px; height: 22px; background: rgba(255, 255, 255, 0.28); }
.tenant-name { white-space: nowrap; }
.header-status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: rgba(255, 255, 255, 0.86); }
.header-status i, .connection-indicator i { width: 7px; height: 7px; border-radius: 50%; background: #7ee0a8; box-shadow: 0 0 0 3px rgba(126, 224, 168, 0.18); }
.icon-button, .user-button { border: 0; color: inherit; background: transparent; cursor: pointer; }
.icon-button { display: inline-grid; width: 32px; height: 32px; place-items: center; border-radius: var(--supos-radius-sm); }
.icon-button:hover, .icon-button:focus-visible { background: rgba(255, 255, 255, 0.14); outline: none; }
.icon-button :deep(svg) { width: 17px; height: 17px; }
.user-button { gap: 7px; padding: 6px 8px; border-radius: var(--supos-radius-sm); font-size: 12px; }
.user-button:hover, .user-button:focus-visible { background: rgba(255, 255, 255, 0.14); outline: none; }
.user-button :deep(svg) { width: 16px; height: 16px; }

.app-sidebar {
  position: fixed;
  inset: var(--supos-header-height) auto 0 0;
  z-index: 20;
  width: var(--supos-sidebar-width);
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--supos-line);
  background: #f8fafc;
  transition: width 0.2s ease, transform 0.2s ease;
}
.sidebar-toolbar { gap: 8px; padding: 14px 14px 12px; border-bottom: 1px solid var(--supos-line); }
.sidebar-search { display: flex; align-items: center; flex: 1; gap: 8px; height: 34px; padding: 0 9px; border: 1px solid var(--supos-line); border-radius: var(--supos-radius-sm); background: var(--supos-panel); color: var(--supos-subtle); }
.sidebar-search :deep(svg) { width: 15px; height: 15px; flex-shrink: 0; }
.sidebar-search input { width: 100%; min-width: 0; border: 0; outline: 0; color: var(--supos-text); background: transparent; font-size: 13px; }
.sidebar-search input::placeholder { color: var(--supos-subtle); }
.sidebar-toggle { color: var(--supos-muted); flex-shrink: 0; }
.sidebar-toggle:hover, .sidebar-toggle:focus-visible { background: var(--supos-blue-050); color: var(--supos-blue-600); }
.sidebar-nav { flex: 1; overflow: auto; padding: 16px 10px; }
.nav-group + .nav-group { margin-top: 20px; }
.nav-group-label { padding: 0 10px 7px; color: var(--supos-subtle); font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; }
.sidebar-menu { width: 100%; border-right: 0; background: transparent; --el-menu-bg-color: transparent; --el-menu-text-color: var(--supos-muted); --el-menu-active-color: var(--supos-blue-700); --el-menu-hover-bg-color: var(--supos-blue-050); }
:deep(.sidebar-menu .el-menu) { background: transparent; }
:deep(.sidebar-menu .el-menu-item),
:deep(.sidebar-menu .el-sub-menu__title) { min-width: 0; height: 36px; margin: 1px 0; border-left: 2px solid transparent; border-radius: var(--supos-radius-sm); line-height: 36px; color: var(--supos-muted); font-size: 13px; }
:deep(.sidebar-menu .el-menu-item:hover),
:deep(.sidebar-menu .el-sub-menu__title:hover) { background: var(--supos-blue-050); color: var(--supos-text); }
:deep(.sidebar-menu .el-menu-item.is-active) { border-left-color: var(--supos-blue-600); background: var(--supos-blue-050); color: var(--supos-blue-700); font-weight: 600; }
:deep(.sidebar-menu .el-sub-menu.is-active > .el-sub-menu__title) { color: var(--supos-blue-700); }
:deep(.sidebar-menu .el-menu-item:focus-visible),
:deep(.sidebar-menu .el-sub-menu__title:focus-visible) { outline: 2px solid var(--supos-blue-500); outline-offset: -2px; }
:deep(.sidebar-menu svg) { color: var(--supos-subtle); }
:deep(.sidebar-menu .el-menu-item.is-active svg),
:deep(.sidebar-menu .el-sub-menu.is-active > .el-sub-menu__title svg) { color: var(--supos-blue-600); }
.sidebar-empty { padding: 18px 10px; color: var(--supos-subtle); font-size: 12px; text-align: center; }
.sidebar-footer { display: grid; gap: 5px; padding: 13px 18px; border-top: 1px solid var(--supos-line); }
.connection-indicator { gap: 8px; color: var(--supos-muted); font-size: 12px; }
.sidebar-footer-version { color: var(--supos-subtle); font-family: var(--supos-font-mono); font-size: 10px; }

.app-main { min-height: 100vh; padding-left: var(--supos-sidebar-width); padding-top: var(--supos-header-height); transition: padding-left 0.2s ease; }
.workspace-toolbar { min-height: 52px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 28px; border-bottom: 1px solid var(--supos-line); background: var(--supos-panel); }
.breadcrumbs { gap: 9px; color: var(--supos-subtle); font-size: 12px; }
.breadcrumbs b { color: var(--supos-line-strong); font-weight: 400; }
.breadcrumbs strong { color: var(--supos-text); font-weight: 600; }
.workspace-hint { max-width: 520px; overflow: hidden; color: var(--supos-subtle); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.workspace-content { max-width: 1440px; margin: 0 auto; padding: 28px; }

.is-sidebar-collapsed .app-sidebar { width: 68px; }
.is-sidebar-collapsed .app-main { padding-left: 68px; }
.is-sidebar-collapsed .sidebar-search { display: none; }
.is-sidebar-collapsed .sidebar-toolbar { justify-content: center; }
.is-sidebar-collapsed .sidebar-nav { padding-inline: 2px; }
.is-sidebar-collapsed .nav-group-label, .is-sidebar-collapsed .connection-indicator span, .is-sidebar-collapsed .sidebar-footer-version { display: none; }
.is-sidebar-collapsed .sidebar-footer { justify-items: center; padding: 13px 0; }

:global(.sidebar-menu-popper) { border: 1px solid var(--supos-line); border-radius: var(--supos-radius-sm); box-shadow: var(--supos-shadow-soft); }
:global(.sidebar-menu-popper .el-menu) { background: var(--supos-panel); }

.mobile-scrim { display: none; }

@media (max-width: 960px) {
  .app-header { padding: 0 16px; }
  .header-menu-button { display: inline-grid !important; }
  .app-sidebar { width: 288px; transform: translateX(-100%); box-shadow: var(--supos-shadow-soft); }
  .is-mobile-open .app-sidebar { transform: translateX(0); }
  .app-main, .is-sidebar-collapsed .app-main { padding-left: 0; }
  .is-sidebar-collapsed .app-sidebar { width: 288px; }
  .is-sidebar-collapsed .sidebar-nav { padding: 16px 10px; }
  .is-sidebar-collapsed .sidebar-search, .is-sidebar-collapsed .nav-group-label, .is-sidebar-collapsed .connection-indicator span, .is-sidebar-collapsed .sidebar-footer-version { display: initial; }
  .is-sidebar-collapsed .sidebar-toolbar { justify-content: initial; }
  .is-sidebar-collapsed .sidebar-footer { justify-items: initial; padding: 13px 18px; }
  .mobile-scrim { position: fixed; inset: var(--supos-header-height) 0 0; z-index: 15; display: block; border: 0; background: rgba(20, 34, 50, 0.28); }
  .header-brand-copy span, .header-divider, .tenant-name, .header-status, .header-version { display: none; }
  .workspace-content { padding: 22px 18px; }
}

@media (max-width: 640px) {
  .app-header { gap: 8px; }
  .header-brand-copy strong { max-width: 150px; }
  .header-actions { gap: 2px; }
  .user-button span { display: none; }
  .workspace-toolbar { padding: 0 16px; }
  .workspace-hint { display: none; }
  .workspace-content { padding: 16px; }
}
</style>
