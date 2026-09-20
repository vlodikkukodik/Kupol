// Общий клиент TanStack Query: кэш ответов, повторные запросы, дедупликация. Ошибки «нет права», «нет такого»,
// «не вошли» повторять бессмысленно, а сбой сети и 5xx — стоит один раз.
import { QueryClient } from '@tanstack/vue-query'
import { ApiError } from './client'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          if (failureCount >= 1) return false
          return error instanceof ApiError && (error.status === 0 || error.status >= 500) && error.code !== 'maintenance'
        },
      },
    },
  })
}

/** Ключи кэша: в одном месте, чтобы инвалидация не расходилась с запросами. */
export const keys = {
  recent: (limit: number) => ['documents', 'recent', limit] as const,
  summary: ['documents', 'summary'] as const,
  list: (query: string) => ['documents', 'list', query] as const,
  search: (query: string) => ['documents', 'search', query] as const,
  graph: (ref: string, depth: number) => ['documents', 'graph', ref, depth] as const,
  document: (ref: string) => ['documents', 'item', ref] as const,
  teamMeta: ['team', 'meta'] as const,
  teamRoles: ['team', 'roles'] as const,
  teamMembers: (params: string) => ['team', 'members', params] as const,
  teamDocuments: (params: string) => ['team', 'documents', params] as const,
  teamDashboard: ['team', 'dashboard'] as const,
  totp: ['me', 'totp'] as const,
  teamTemplates: (kind: string) => ['team', 'templates', kind] as const,
  teamTemplate: (id: number) => ['team', 'template', id] as const,
  teamGlossary: (q: string) => ['team', 'glossary', q] as const,
  teamTimeline: ['team', 'timeline'] as const,
  teamSite: ['team', 'site'] as const,
  site: ['documents', 'site'] as const,
  timeline: ['documents', 'timeline'] as const,
  teamDocument: (id: number) => ['team', 'document', id] as const,
  teamVersions: (id: number, page: number) => ['team', 'versions', id, page] as const,
  // рецензия и линтер зависят от редакции и статуса: смена любого из них — новый запрос
  teamReview: (id: number, revision: number, status: string) => ['team', 'review', id, revision, status] as const,
  teamLint: (id: number, revision: number, status: string) => ['team', 'lint', id, revision, status] as const,
}
