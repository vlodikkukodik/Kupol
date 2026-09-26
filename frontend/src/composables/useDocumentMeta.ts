import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import type { Meta, Option } from '@/api/generated/documents'

// Справочник для форм команды (типы, статусы, свойства Объекта, типы блоков) — один на сервере, здесь только читается.
// Не меняется, пока открыт сайт: запрос общий на все экраны и не повторяется.
export function useDocumentMeta() {
  const query = useQuery({ queryKey: keys.teamMeta, queryFn: ({ signal }) => teamApi.meta({ signal }), staleTime: Infinity })
  const meta = computed<Meta | undefined>(() => query.data.value)
  const nameOf = (list: 'statuses' | 'block_kinds' | 'categories' | 'containment' | 'direct_links', id: string): string =>
    (meta.value?.[list] as Option[] | undefined)?.find((o) => o.id === id)?.name ?? id
  return {
    meta,
    query,
    statusName: (id: string) => nameOf('statuses', id),
    typeName: (id: string) => meta.value?.types.find((t) => t.id === id)?.name ?? id,
    blockKindName: (id: string) => nameOf('block_kinds', id),
    types: computed(() => meta.value?.types ?? []),
    statuses: computed(() => meta.value?.statuses ?? []),
  }
}
