import { computed } from 'vue'
import { api } from '../api/index.js'
import { useResource } from './useResource.js'

// Справочник для форм команды (типы, статусы, свойства Объекта, типы блоков) — один на сервере, здесь только читается.
// Запрос общий на все экраны: справочник не меняется, пока открыт сайт.
let shared = null

export function useDocumentMeta() {
  const res = useResource(() => {
    shared ??= api.get('/team/document-types').catch((err) => {
      shared = null // не удалось — в следующий раз спросить снова
      throw err
    })
    return shared
  })
  const nameOf = (list, id) => res.data.value?.[list]?.find((o) => o.id === id)?.name ?? id
  return {
    meta: res.data,
    error: res.error,
    loading: res.loading,
    reload: res.reload,
    statusName: (id) => nameOf('statuses', id),
    typeName: (id) => nameOf('types', id),
    blockKindName: (id) => nameOf('block_kinds', id),
    types: computed(() => res.data.value?.types ?? []),
    statuses: computed(() => res.data.value?.statuses ?? []),
  }
}

/** Для тестов: забыть сохранённый ответ. */
export function resetDocumentMetaCache() {
  shared = null
}
