import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import { useAuthStore } from '../stores/auth.js'

const BASE_TITLE = 'КУПОЛ'

export const routes = [
  // needsApi: при сбое связи с архивом вместо страницы показывается «Сбой архива»
  // requiresAuth: только для вошедших; guestOnly: только для тех, кто не вошёл
  { path: '/', name: 'home', component: HomeView, meta: { title: 'КУПОЛ — Центральный архив', needsApi: false } },
  {
    path: '/catalog',
    name: 'catalog',
    component: () => import('../views/CatalogView.vue'),
    meta: { title: `Каталог — ${BASE_TITLE}`, needsApi: true },
  },
  {
    path: '/doc/:ref',
    name: 'document',
    component: () => import('../views/DocumentView.vue'),
    meta: { title: `Документ — ${BASE_TITLE}`, needsApi: true },
  },
  {
    path: '/file',
    name: 'file',
    component: () => import('../views/FileView.vue'),
    meta: { title: `Личное дело — ${BASE_TITLE}`, needsApi: true, requiresAuth: true },
  },
  {
    // Панель команды: право проверяется охраной страниц (и, главное, сервером на каждом запросе).
    path: '/team',
    component: () => import('../views/team/TeamView.vue'),
    meta: { title: `Панель команды — ${BASE_TITLE}`, needsApi: true, requiresAuth: true, capability: 'team_panel' },
    children: [
      { path: '', name: 'team', component: () => import('../views/team/TeamHomeView.vue') },
      {
        path: 'documents',
        name: 'team-documents',
        component: () => import('../views/team/TeamDocumentsView.vue'),
        meta: { title: `Документы команды — ${BASE_TITLE}` },
      },
      {
        path: 'documents/new',
        name: 'team-document-new',
        component: () => import('../views/team/TeamDocumentNewView.vue'),
        meta: { title: `Новый документ — ${BASE_TITLE}`, capability: 'write_drafts' },
      },
      {
        // Номер — только цифры: иначе адрес вида /team/documents/abc показал бы «Дело не найдено» самого роутера
        path: 'documents/:id(\\d+)',
        name: 'team-document',
        component: () => import('../views/team/TeamDocumentView.vue'),
        meta: { title: `Документ — Панель команды — ${BASE_TITLE}` },
      },
      {
        path: 'members',
        name: 'team-members',
        component: () => import('../views/team/TeamMembersView.vue'),
        meta: { title: `Команда — ${BASE_TITLE}`, capability: 'manage_team' },
      },
    ],
  },
  {
    path: '/backup-code',
    name: 'backup-code',
    component: () => import('../views/BackupCodeView.vue'),
    meta: { title: `Резервный код — ${BASE_TITLE}`, needsApi: true, requiresAuth: true },
  },
  {
    path: '/restore',
    name: 'restore',
    component: () => import('../views/RestoreView.vue'),
    meta: { title: `Восстановление доступа — ${BASE_TITLE}`, needsApi: true, guestOnly: true },
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('../views/NotFoundView.vue'),
    meta: { title: `Дело не найдено — ${BASE_TITLE}`, needsApi: false },
  },
]

/**
 * Безопасный путь возврата после входа: только внутренний путь этого сайта.
 * Отвергает абсолютные адреса, «//host» и обратные слеши (открытое перенаправление).
 */
export function safeNextPath(next) {
  if (typeof next !== 'string') return null
  if (!next.startsWith('/') || next.startsWith('//') || next.includes('\\')) return null
  if (Array.from(next).some((ch) => ch.charCodeAt(0) < 32)) return null // управляющие символы (перевод строки и т.п.)
  return next
}

/** Охрана страниц: кто вошёл, тот на /file, кто нет — на главной. */
export async function authGuard(to) {
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
  const needed = to.matched.map((r) => r.meta.capability).filter(Boolean)
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

export function createAppRouter(history = createWebHistory()) {
  const router = createRouter({
    history,
    routes,
    scrollBehavior: (to, from, saved) => saved || { top: 0 },
  })
  router.beforeEach(authGuard)
  router.afterEach((to) => {
    document.title = to.meta.title || BASE_TITLE
  })
  return router
}
