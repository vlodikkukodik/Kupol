import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { guest: true } },
    { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { guest: true } },
    {
      path: '/',
      component: () => import('@/views/AppLayout.vue'),
      meta: { auth: true },
      children: [
        { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'sites', name: 'sites', component: () => import('@/views/SitesView.vue') },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
      ],
    },
    // Кабинет выбранного сайта: свой каркас (SiteLayout) вместо общего меню панели.
    {
      path: '/sites/:id(\\d+)',
      component: () => import('@/views/site/SiteLayout.vue'),
      meta: { auth: true },
      children: [
        { path: '', name: 'site-overview', component: () => import('@/views/site/SiteOverview.vue') },
        { path: 'files', name: 'files', component: () => import('@/views/FilesView.vue') },
        { path: 'domains', name: 'site-domains', component: () => import('@/views/site/SiteDomains.vue') },
        { path: 'stats', name: 'site-stats', component: () => import('@/views/site/SiteStats.vue') },
        { path: 'logs', name: 'site-logs', component: () => import('@/views/site/SiteLogs.vue') },
        { path: 'ftp', name: 'site-ftp', component: () => import('@/views/site/SiteFtp.vue') },
        { path: 'settings', name: 'site-settings', component: () => import('@/views/site/SiteSettings.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  await auth.init()
  if (to.meta.auth && !auth.user) return { name: 'login', query: to.fullPath === '/' ? {} : { next: to.fullPath } }
  if (to.meta.guest && auth.user) return { name: 'dashboard' }
})
