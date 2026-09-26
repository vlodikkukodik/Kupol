import { computed, type Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { TeamDocument } from '@/api/generated/documents'
import { keys } from '@/api/query'

/**
 * Рецензия и линтер документа: два запроса, общие для вкладки «Рецензия» и счётчика открытых комментариев на её кнопке
 * (одинаковые ключи — один запрос на двоих). Линтер нужен только вкладке: счётчику он не запрашивается (withLint: false). Ключ включает редакцию и статус: после сохранения или перехода документа
 * данные запрашиваются заново, без ручных сбросов.
 */
export function useReview(doc: Ref<Pick<TeamDocument, 'id' | 'revision' | 'status'> | null>, { withLint = true } = {}) {
  const enabled = computed(() => doc.value !== null)
  const lintEnabled = computed(() => enabled.value && withLint)
  const review = useQuery({
    queryKey: computed(() => keys.teamReview(doc.value?.id ?? 0, doc.value?.revision ?? 0, doc.value?.status ?? '')),
    queryFn: ({ signal }) => teamApi.review(doc.value!.id, { signal }).then((r) => r.review),
    enabled,
    retry: false,
    staleTime: 0,
  })
  const lint = useQuery({
    queryKey: computed(() => keys.teamLint(doc.value?.id ?? 0, doc.value?.revision ?? 0, doc.value?.status ?? '')),
    queryFn: ({ signal }) => teamApi.lint(doc.value!.id, { signal }).then((r) => r.lint),
    enabled: lintEnabled,
    retry: false,
    staleTime: 0,
  })
  /** Сколько комментариев ещё не отмечено исправленными (для счётчика на вкладке) */
  const open = computed(() => review.data.value?.open ?? 0)
  const error = computed(() => (isApiError(review.error.value) ? review.error.value : isApiError(lint.error.value) ? lint.error.value : null))
  return { review, lint, open, error }
}
