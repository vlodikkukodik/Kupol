import { defineStore } from 'pinia'
import { api } from '../api/index.js'

/**
 * Как исход запроса влияет на состояние связи с архивом.
 * Обычные ошибки (401, 404, 422, даже 500 конкретного обработчика) — связь есть.
 * @param {import('../api/client.js').ApiError} error
 * @returns {'maintenance'|'outage'|'online'}
 */
export function classifyError(error) {
  if (error.code === 'maintenance') return 'maintenance'
  const infra = [0, 502, 503, 504].includes(error.status) || String(error.code).startsWith('proxy_')
  return infra ? 'outage' : 'online'
}

export const useConnectionStore = defineStore('connection', {
  state: () => ({
    /** unknown | online | outage | maintenance */
    state: 'unknown',
    requestId: '',
    checking: false,
  }),
  actions: {
    /** Применить исход любого API-запроса. */
    apply(event) {
      if (event.ok) {
        this.state = 'online'
        this.requestId = ''
        return
      }
      this.state = classifyError(event.error)
      this.requestId = this.state === 'online' ? '' : event.error.requestId
    },
    /** Подписаться на все запросы клиента. Возвращает функцию отписки. */
    attach(client = api) {
      return client.subscribe((event) => this.apply(event))
    },
    /** Явная проверка связи (/api/health). Состояние обновится через подписку. */
    async check() {
      if (this.checking) return
      this.checking = true
      try {
        await api.get('/health')
      } catch {
        // исход уже учтён подпиской attach()
      } finally {
        this.checking = false
      }
    },
  },
})
