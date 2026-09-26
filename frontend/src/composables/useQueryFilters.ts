import { watch } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import { withFilters } from '@/lib/catalog'

/**
 * Изменение условий списка (фильтры, поиск) через адрес страницы.
 *
 * Переход по адресу завершается не сразу (охрана страниц, ленивые экраны), а изменения бывают подряд — например,
 * читатель ввёл год «с» и тут же перешёл к полю «по». Если строить второй адрес от ещё не обновлённого `route.query`,
 * первое изменение потеряется. Поэтому незавершённые изменения копятся в `pending`, а сбрасываются, когда адрес
 * действительно сменился.
 */
export function useQueryFilters() {
  const route = useRoute()
  const router = useRouter()
  let pending: LocationQueryRaw | null = null

  watch(
    () => route.fullPath,
    () => {
      pending = null
    },
  )

  return {
    /** Добавить, изменить или убрать (пустое значение) условия; страница сбрасывается. */
    change(changes: Record<string, string | null | undefined>) {
      pending = withFilters(pending ?? route.query, changes)
      return router.push({ query: pending })
    },
  }
}
