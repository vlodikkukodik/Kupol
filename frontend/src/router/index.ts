import { createRouter, createWebHistory, type RouteLocationNormalized, type RouteLocationRaw, type RouteRecordRaw, type RouterHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'
import { locale, t } from '@/i18n'
import { useAuthStore, type Capability } from '@/stores/auth'
import { watch } from 'vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Ключ названия страницы в каталоге языка (title.*); заголовок вкладки — «Название — КУПОЛ» */
    title?: string
    /** Ключ уже полного заголовка вкладки (главная и «О КУПОЛЕ»): «КУПОЛ — Центральный архив» */
    fullTitle?: string
    /** При сбое связи с архивом вместо страницы показывается «Сбой архива» */
    needsApi?: boolean
    /** Только для вошедших */
    requiresAuth?: boolean
    /** Только для тех, кто не вошёл */
    guestOnly?: boolean
    /** Право, без которого страница «не существует» */
    capability?: Capability
  }
}

/** Заголовок вкладки для маршрута на текущем языке */
export function pageTitle(meta: { title?: string; fullTitle?: string }): string {
  if (meta.fullTitle) return t(meta.fullTitle)
  if (meta.title) return t('title.withBase', { name: t(meta.title) })
  return t('title.base')
}

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: HomeView, meta: { fullTitle: 'title.home', needsApi: false } },
  { path: '/catalog', name: 'catalog', component: () => import('@/views/CatalogView.vue'), meta: { title: 'title.catalog', needsApi: true } },
  { path: '/about', name: 'about', component: () => import('@/views/AboutView.vue'), meta: { fullTitle: 'title.about', needsApi: false } },
  { path: '/search', name: 'search', component: () => import('@/views/SearchView.vue'), meta: { title: 'title.search', needsApi: true } },
  { path: '/graph/:ref', name: 'graph', component: () => import('@/views/GraphView.vue'), meta: { title: 'title.graph', needsApi: true } },
  { path: '/doc/:ref', name: 'document', component: () => import('@/views/DocumentView.vue'), meta: { title: 'title.document', needsApi: true } },
  { path: '/file', name: 'file', component: () => import('@/views/FileView.vue'), meta: { title: 'title.file', needsApi: true, requiresAuth: true } },
  { path: '/inbox', name: 'inbox', component: () => import('@/views/InboxView.vue'), meta: { title: 'title.inbox', needsApi: true, requiresAuth: true } },
  { path: '/user/:login', name: 'user', component: () => import('@/views/UserCardView.vue'), meta: { title: 'title.userCard', needsApi: true, requiresAuth: true } },
  {
    // «Предложения» (шаг 5.4): форма и собственные предложения только для вошедших; очередь — в панели команды.
    path: '/suggestions',
    name: 'suggestions',
    component: () => import('@/views/SuggestionsView.vue'),
    meta: { title: 'title.suggestions', needsApi: true, requiresAuth: true },
  },
  {
    // Панель команды: право проверяется охраной страниц (и, главное, сервером на каждом запросе).
    path: '/team',
    component: () => import('@/views/team/TeamView.vue'),
    meta: { title: 'title.team', needsApi: true, requiresAuth: true, capability: 'team_panel' },
    children: [
      { path: '', name: 'team', component: () => import('@/views/team/TeamDeskView.vue'), meta: { title: 'title.teamDesk' } },
      { path: 'templates', name: 'team-templates', component: () => import('@/views/team/TeamTemplatesView.vue'), meta: { title: 'title.teamTemplates' } },
      { path: 'site', name: 'team-site', component: () => import('@/views/team/TeamSiteView.vue'), meta: { title: 'title.teamSite' } },
      { path: 'timeline', name: 'team-timeline', component: () => import('@/views/team/TeamTimelineView.vue'), meta: { title: 'title.teamTimeline' } },
      { path: 'glossary', name: 'team-glossary', component: () => import('@/views/team/TeamGlossaryView.vue'), meta: { title: 'title.teamGlossary' } },
      { path: 'uploads', name: 'team-uploads', component: () => import('@/views/team/TeamUploadsView.vue'), meta: { title: 'title.teamUploads', capability: 'write_drafts' } },
      { path: 'sanctions', name: 'team-sanctions', component: () => import('@/views/team/TeamSanctionsView.vue'), meta: { title: 'title.teamSanctions', capability: 'moderate_comments' } },
      { path: 'petitions', name: 'team-petitions', component: () => import('@/views/team/TeamPetitionsView.vue'), meta: { title: 'title.teamPetitions' } },
      { path: 'inbox', name: 'team-inbox', component: () => import('@/views/team/TeamInboxView.vue'), meta: { title: 'title.teamInbox' } },
      { path: 'secret-codes', name: 'team-secret-codes', component: () => import('@/views/team/TeamSecretCodesView.vue'), meta: { title: 'title.teamSecretCodes' } },
      { path: 'roles', name: 'team-roles', component: () => import('@/views/team/TeamHomeView.vue'), meta: { title: 'title.teamRoles' } },
      { path: 'documents', name: 'team-documents', component: () => import('@/views/team/TeamDocumentsView.vue'), meta: { title: 'title.teamDocuments' } },
      {
        path: 'documents/new',
        name: 'team-document-new',
        component: () => import('@/views/team/TeamDocumentNewView.vue'),
        meta: { title: 'title.teamNew', capability: 'write_drafts' },
      },
      {
        // Номер — только цифры: иначе адрес вида /team/documents/abc показал бы «Дело не найдено» самого роутера
        path: 'documents/:id(\\d+)',
        name: 'team-document',
        component: () => import('@/views/team/TeamDocumentView.vue'),
        meta: { title: 'title.teamDocument' },
      },
      { path: 'members', name: 'team-members', component: () => import('@/views/team/TeamMembersView.vue'), meta: { title: 'title.teamMembers', capability: 'manage_team' } },
      { path: 'reports', name: 'team-reports', component: () => import('@/views/team/TeamReportsView.vue'), meta: { title: 'title.teamReports', capability: 'moderate_comments' } },
      { path: 'suggestions', name: 'team-suggestions', component: () => import('@/views/team/TeamSuggestionsView.vue'), meta: { title: 'title.teamSuggestions', capability: 'review' } },
    ],
  },
  { path: '/backup-code', name: 'backup-code', component: () => import('@/views/BackupCodeView.vue'), meta: { title: 'title.backupCode', needsApi: true, requiresAuth: true } },
  { path: '/restore', name: 'restore', component: () => import('@/views/RestoreView.vue'), meta: { title: 'title.restore', needsApi: true, guestOnly: true } },
  { path: '/email-confirm', name: 'email-confirm', component: () => import('@/views/EmailConfirmView.vue'), meta: { title: 'title.emailConfirm', needsApi: true } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue'), meta: { title: 'title.notFound', needsApi: false } },
]

/**
 * Безопасный путь возврата после входа: только внутренний путь этого сайта.
 * Отвергает абсолютные адреса, «//host» и обратные слеши (открытое перенаправление).
 */
export function safeNextPath(next: unknown): string | null {
  if (typeof next !== 'string') return null
  if (!next.startsWith('/') || next.startsWith('//') || next.includes('\\')) return null
  if (Array.from(next).some((ch) => ch.charCodeAt(0) < 32)) return null // управляющие символы (перевод строки и т.п.)
  return next
}

/** Охрана страниц: кто вошёл, тот на /file, кто нет — на главной. */
export async function authGuard(to: RouteLocationNormalized): Promise<true | RouteLocationRaw> {
  const auth = useAuthStore()
  if (auth.status !== 'ready') {
    try {
      await auth.load()
    } catch {
      // Нет связи: пропускаем, App покажет «Сбой архива» для страниц, которым нужен API.
    }
  }
  // Страница, требующая права: права могли измениться (или человек вышел в другой вкладке), пока сайт был открыт,
  // поэтому перед любой проверкой сессия перечитывается у сервера.
  const needed = to.matched.map((r) => r.meta.capability).filter((c): c is Capability => Boolean(c))
  if (needed.length > 0 && auth.user) {
    try {
      await auth.load(true)
    } catch {
      // Нет связи: решает то, что уже известно; App покажет «Сбой архива».
    }
  }
  if (to.meta.requiresAuth && auth.status === 'ready' && !auth.user) {
    // ?next= — и куда вернуть после входа, и знак для App: открыть окно входа поверх главной
    return { name: 'home', query: { next: to.fullPath } }
  }
  if (to.meta.guestOnly && auth.user) return { name: 'file' }
  // Без права страницу не раскрываем: показывается «Дело не найдено» по тому же адресу.
  if (auth.user && needed.some((c) => !auth.can(c))) {
    return { name: 'not-found', params: { pathMatch: to.path.split('/').slice(1) }, query: to.query, hash: to.hash }
  }
  // Экран резервного кода имеет смысл, только пока код не подтверждён
  if (to.name === 'backup-code' && !auth.pendingBackupCode) return { name: 'file' }
  return true
}

export function createAppRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({
    history,
    routes,
    // Якоря «О КУПОЛЕ» (#privacy, #timeline) — статичные разделы страницы; якорь блока документа (#b-…) появляется после загрузки данных, им занят сам документ.
    scrollBehavior: (to, _from, saved) => saved || (to.name === 'about' && to.hash ? { el: to.hash } : { top: 0 }),
  })
  router.beforeEach(authGuard)
  router.afterEach((to) => {
    document.title = pageTitle(to.meta)
  })
  // Сменили язык — заголовок вкладки пересчитывается. Страницы, которые ставят свой заголовок (документ), делают то же у себя.
  const OWN_TITLE = ['document', 'team-document']
  watch(locale, () => {
    const r = router.currentRoute.value
    if (r.matched.length > 0 && !OWN_TITLE.includes(String(r.name)) && (r.meta.title || r.meta.fullTitle)) document.title = pageTitle(r.meta)
  })
  return router
}
