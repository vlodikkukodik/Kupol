import { onMounted, onBeforeUnmount } from 'vue'
import { useConnectionStore } from '../stores/connection.js'

/** Проверяет связь с архивом при старте, раз в минуту и при возврате на вкладку. */
export function useConnectionMonitor(intervalMs = 60_000) {
  const connection = useConnectionStore()
  let timer = null

  const tick = () => {
    if (document.visibilityState === 'visible') connection.check()
  }

  onMounted(() => {
    connection.check()
    timer = setInterval(tick, intervalMs)
    document.addEventListener('visibilitychange', tick)
  })
  onBeforeUnmount(() => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', tick)
  })
}
