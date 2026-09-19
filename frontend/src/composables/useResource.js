import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { ApiError } from '../api/client.js'

/**
 * Загрузка данных с состояниями «идёт / готово / ошибка». Устаревший запрос отменяется, когда
 * зависимости изменились или компонент ушёл: старый ответ не перезапишет новый.
 *
 * @param {(signal: AbortSignal) => Promise<any>} load  функция загрузки
 * @param {() => any} [source]  что отслеживать: при изменении данные загружаются заново
 */
export function useResource(load, source = () => null) {
  const data = shallowRef(null)
  const error = shallowRef(null)
  const loading = ref(true)
  let controller = null
  let seq = 0

  async function reload() {
    controller?.abort()
    controller = new AbortController()
    const mine = ++seq
    loading.value = true
    error.value = null
    try {
      const result = await load(controller.signal)
      if (mine === seq) data.value = result
    } catch (err) {
      if (mine !== seq || controller.signal.aborted) return // ответ устарел
      if (!(err instanceof ApiError)) throw err
      data.value = null
      error.value = err
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  watch(source, reload, { immediate: true })
  onBeforeUnmount(() => {
    seq++
    controller?.abort()
  })

  return { data, error, loading, reload }
}
