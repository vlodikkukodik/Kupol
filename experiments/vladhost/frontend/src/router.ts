import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { guest: true } },
    { path: '/forgot', name: 'forgot', component: () => import('@/views/ForgotView.vue'), meta: { guest: true } },
    // Сброс пароля и подтверждение адреса открываются по ссылке из письма — и без входа, и во вкладке, где вход уже есть.
    { path: '/reset-password', name: 'reset-password', component: () => import('@/views/ResetView.vue') },
    { path: '/verify-email', name: 'verify-email', component: () => import('@/views/VerifyEmailView.vue') },
    { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { guest: true } },
    {
      path: '/',
      component: () => import('@/views/AppLayout.vue'),
      meta: { auth: true },
      children: [
        { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'sites', name: 'sites', component: () => import('@/views/SitesView.vue') },
        { path: 'cron', name: 'cron', component: () => import('@/views/CronView.vue') },
        { path: 'dns', name: 'dns', component: () => import('@/views/DnsView.vue') },
        { path: 'activity', name: 'activity', component: () => import('@/views/ActivityView.vue') },
        { path: 'support', name: 'support', component: () => import('@/views/SupportView.vue') },
        { path: 'support/:id(\\d+)', name: 'ticket', component: () => import('@/views/TicketView.vue') },
        { path: 'help/:slug?', name: 'help', component: () => import('@/views/HelpView.vue') },
        { path: 'mail', name: 'mailhost', component: () => import('@/views/MailView.vue') },
        { path: 'ssh', name: 'ssh', component: () => import('@/views/SshKeysView.vue') },
        { path: 'databases', name: 'databases', component: () => import('@/views/DatabasesView.vue') },
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
        { path: 'ssl', name: 'site-ssl', component: () => import('@/views/site/SiteSsl.vue') },
        { path: 'stats', name: 'site-stats', component: () => import('@/views/site/SiteStats.vue') },
        { path: 'runtime', name: 'site-runtime', component: () => import('@/views/site/SiteRuntime.vue') },
        { path: 'terminal', name: 'site-shell', component: () => import('@/views/site/SiteShell.vue') },
        { path: 'apps', name: 'site-cms', component: () => import('@/views/site/SiteCms.vue') },
        { path: 'logs', name: 'site-logs', component: () => import('@/views/site/SiteLogs.vue') },
        { path: 'backups', name: 'site-backups', component: () => import('@/views/site/SiteBackups.vue') },
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
