import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api'
import type { ApiError, ApiEvent } from '@/api'
import { healthApi } from '@/api/endpoints'

export type ConnectionState = 'unknown' | 'online' | 'outage' | 'maintenance'

/**
 * Как исход запроса влияет на состояние связи с архивом.
 * Обычные ошибки (401, 404, 422, даже 500 конкретного обработчика) — связь есть.
 */
export function classifyError(error: ApiError): 'maintenance' | 'outage' | 'online' {
  if (error.code === 'maintenance') return 'maintenance'
  const infra = [0, 502, 503, 504].includes(error.status) || error.code.startsWith('proxy_')
  return infra ? 'outage' : 'online'
}

export const useConnectionStore = defineStore('connection', () => {
  const state = ref<ConnectionState>('unknown')
  const requestId = ref('')
  const checking = ref(false)

  /** Применить исход любого API-запроса. */
  function apply(event: ApiEvent) {
    if (event.ok) {
      state.value = 'online'
      requestId.value = ''
      return
    }
    const next = classifyError(event.error)
    state.value = next
    requestId.value = next === 'online' ? '' : event.error.requestId
  }

  /** Подписаться на все запросы клиента. Возвращает функцию отписки. */
  function attach(client = api) {
    return client.subscribe(apply)
  }

  /** Явная проверка связи (/api/health). Состояние обновится через подписку. */
  async function check() {
    if (checking.value) return
    checking.value = true
    try {
      await healthApi.check()
    } catch {
      // исход уже учтён подпиской attach()
    } finally {
      checking.value = false
    }
  }

  return { state, requestId, checking, apply, attach, check }
})
