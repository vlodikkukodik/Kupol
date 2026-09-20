import { createRouter, createWebHistory, type RouteLocationNormalized, type RouteLocationRaw, type RouteRecordRaw, type RouterHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'
import { useAuthStore, type Capability } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
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

const BASE_TITLE = 'КУПОЛ'
const title = (name: string) => `${name} — ${BASE_TITLE}`

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: HomeView, meta: { title: 'КУПОЛ — Центральный архив', needsApi: false } },
  { path: '/catalog', name: 'catalog', component: () => import('@/views/CatalogView.vue'), meta: { title: title('Каталог'), needsApi: true } },
  { path: '/doc/:ref', name: 'document', component: () => import('@/views/DocumentView.vue'), meta: { title: title('Документ'), needsApi: true } },
  { path: '/file', name: 'file', component: () => import('@/views/FileView.vue'), meta: { title: title('Личное дело'), needsApi: true, requiresAuth: true } },
  {
    // Панель команды: право проверяется охраной страниц (и, главное, сервером на каждом запросе).
    path: '/team',
    component: () => import('@/views/team/TeamView.vue'),
    meta: { title: title('Панель команды'), needsApi: true, requiresAuth: true, capability: 'team_panel' },
    children: [
      { path: '', name: 'team', component: () => import('@/views/team/TeamDeskView.vue'), meta: { title: title('Рабочий стол — Панель команды') } },
      { path: 'roles', name: 'team-roles', component: () => import('@/views/team/TeamHomeView.vue'), meta: { title: title('Роли и права — Панель команды') } },
      { path: 'documents', name: 'team-documents', component: () => import('@/views/team/TeamDocumentsView.vue'), meta: { title: title('Документы команды') } },
      {
        path: 'documents/new',
        name: 'team-document-new',
        component: () => import('@/views/team/TeamDocumentNewView.vue'),
        meta: { title: title('Новый документ'), capability: 'write_drafts' },
      },
      {
        // Номер — только цифры: иначе адрес вида /team/documents/abc показал бы «Дело не найдено» самого роутера
        path: 'documents/:id(\\d+)',
        name: 'team-document',
        component: () => import('@/views/team/TeamDocumentView.vue'),
        meta: { title: title('Документ — Панель команды') },
      },
      { path: 'members', name: 'team-members', component: () => import('@/views/team/TeamMembersView.vue'), meta: { title: title('Команда'), capability: 'manage_team' } },
    ],
  },
  { path: '/backup-code', name: 'backup-code', component: () => import('@/views/BackupCodeView.vue'), meta: { title: title('Резервный код'), needsApi: true, requiresAuth: true } },
  { path: '/restore', name: 'restore', component: () => import('@/views/RestoreView.vue'), meta: { title: title('Восстановление доступа'), needsApi: true, guestOnly: true } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue'), meta: { title: title('Дело не найдено'), needsApi: false } },
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
    scrollBehavior: (_to, _from, saved) => saved || { top: 0 },
  })
  router.beforeEach(authGuard)
  router.afterEach((to) => {
    document.title = to.meta.title || BASE_TITLE
  })
  return router
}
