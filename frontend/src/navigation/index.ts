import type { Component } from 'vue'
import type { RouteRecordRaw } from 'vue-router'
import {
  ChatLineSquare,
  Connection,
  DataBoard,
  Grid,
  Key,
  Setting,
  Tickets,
} from '@element-plus/icons-vue'

export interface NavigationItem {
  /** Stable identifier used by nested menu expansion. Must be unique in the tree. */
  key: string
  /** Text displayed in the sidebar and route breadcrumb. */
  label: string
  /** Element Plus icon displayed before the menu label. */
  icon?: Component
  /** Browser path for an accessible page leaf. */
  path?: string
  /** Unique Vue Router route name for an accessible page leaf. */
  routeName?: string
  /** Lazy page component mounted when the menu leaf is visited. */
  component?: RouteRecordRaw['component']
  /** Nested functional menus; nesting may continue to any practical depth. */
  children?: NavigationItem[]
}

export interface NavigationGroup {
  /** Sidebar section heading used as the first breadcrumb segment. */
  label: string
  /** Functional menu tree rendered under this section. */
  items: NavigationItem[]
}

export const navigationGroups: NavigationGroup[] = [
  {
    label: '网关运营',
    items: [
      {
        key: 'overview',
        path: '/',
        routeName: 'home',
        label: '运行总览',
        icon: DataBoard,
        component: () => import('@/pages/Home.vue'),
      },
      {
        key: 'channels',
        path: '/channels',
        routeName: 'channels',
        label: '渠道管理',
        icon: Connection,
        component: () => import('@/pages/Channels.vue'),
      },
      {
        key: 'models',
        path: '/models',
        routeName: 'models',
        label: '模型路由',
        icon: Grid,
        component: () => import('@/pages/ModelRoutes.vue'),
      },
      {
        key: 'tokens',
        path: '/tokens',
        routeName: 'tokens',
        label: '访问令牌',
        icon: Key,
        component: () => import('@/pages/Tokens.vue'),
      },
      {
        key: 'logs',
        path: '/logs',
        routeName: 'logs',
        label: '调用日志',
        icon: Tickets,
        component: () => import('@/pages/RequestLogs.vue'),
      },
      {
        key: 'sessions',
        path: '/sessions',
        routeName: 'sessions',
        label: '会话日志',
        icon: ChatLineSquare,
        component: () => import('@/pages/CodexSessions.vue'),
      },
    ],
  },
  {
    label: '系统',
    items: [
      {
        key: 'settings',
        path: '/settings',
        routeName: 'settings',
        label: '系统设置',
        icon: Setting,
        component: () => import('@/pages/Settings.vue'),
      },
    ],
  },
]

function collectRoutes(
  groupLabel: string,
  items: NavigationItem[],
  parentLabels: string[] = [],
): RouteRecordRaw[] {
  return items.flatMap((item) => {
    const itemLabels = [...parentLabels, item.label]
    const childRoutes = item.children?.length
      ? collectRoutes(groupLabel, item.children, itemLabels)
      : []

    if (item.children?.length) {
      return childRoutes
    }
    if (!item.path || !item.routeName || !item.component) {
      throw new Error(`功能菜单 ${item.key} 缺少 path、routeName 或 component`)
    }

    const currentRoute: RouteRecordRaw = {
      path: item.path,
      name: item.routeName,
      component: item.component,
      meta: {
        title: item.label,
        breadcrumbs: [groupLabel, ...itemLabels],
        menuKey: item.key,
      },
    }
    return [currentRoute, ...childRoutes]
  })
}

export const navigationRoutes: RouteRecordRaw[] = navigationGroups.flatMap((group) => (
  collectRoutes(group.label, group.items)
))
