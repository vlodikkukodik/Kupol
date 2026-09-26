import { onBeforeUnmount, onMounted } from 'vue'
import { useConnectionStore } from '@/stores/connection'

/** Проверяет связь с архивом при старте, раз в минуту и при возврате на вкладку (только пока вкладка видна). */
export function useConnectionMonitor(intervalMs = 60_000) {
  const connection = useConnectionStore()
  let timer: ReturnType<typeof setInterval> | undefined

  const tick = () => {
    if (document.visibilityState === 'visible') void connection.check()
  }

  onMounted(() => {
    void connection.check()
    timer = setInterval(tick, intervalMs)
    document.addEventListener('visibilitychange', tick)
  })
  onBeforeUnmount(() => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', tick)
  })
}
