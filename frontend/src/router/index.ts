import {createRouter, createWebHistory} from 'vue-router'

const routes = [
    { path: '/', name: 'home', component: () => import('@/pages/Home.vue') },
    { path: '/settings', name: 'settings', component: () => import('@/pages/Settings.vue') },
]

const router = createRouter({
    history: createWebHistory("/static/"),
    routes,
})

export default router
