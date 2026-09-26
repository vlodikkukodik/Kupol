import { onBeforeUnmount, ref } from 'vue'

export type AutosaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error'

/**
 * Автосохранение с задержкой: после каждого изменения ждём `delay` мс тишины и сохраняем.
 * Сохранения не накладываются друг на друга: если изменение пришло, пока идёт запрос, после него будет ещё один.
 *
 * state: idle — нечего сохранять; pending — есть изменения, ждём паузы; saving — идёт запрос; saved — сохранено;
 * error — последняя попытка не удалась (error хранит исключение; следующая правка попробует снова).
 *
 * @param save сохранить текущее состояние (читает его в момент вызова)
 */
export function useAutosave(save: () => Promise<unknown>, { delay = 2000 }: { delay?: number } = {}) {
  const state = ref<AutosaveState>('idle')
  const error = ref<unknown>(null)
  const savedAt = ref<Date | null>(null)
  let timer: ReturnType<typeof setTimeout> | undefined
  let running: Promise<void> | null = null
  let again = false
  let stopped = false

  async function run() {
    state.value = 'saving'
    error.value = null
    try {
      await save()
      savedAt.value = new Date()
      state.value = again ? 'pending' : 'saved'
    } catch (err) {
      error.value = err
      state.value = 'error'
    }
  }

  /** Сохранить сейчас, не дожидаясь паузы. Возвращает, когда всё накопленное записано. */
  async function flush(): Promise<void> {
    clearTimeout(timer)
    timer = undefined
    if (stopped) return
    if (running) {
      again = true
      await running
      return flush()
    }
    if (state.value !== 'pending') return
    again = false
    running = run()
    try {
      await running
    } finally {
      running = null
    }
    // run() мог перевести состояние в error: TypeScript этого не видит, поэтому читаем его заново
    if (again && !stopped && (state.value as AutosaveState) !== 'error') return flush()
  }

  /** Отметить изменение: сохранение произойдёт после паузы. */
  function schedule() {
    if (stopped) return
    if (running) again = true
    state.value = 'pending'
    clearTimeout(timer)
    timer = setTimeout(() => void flush(), delay)
  }

  /** Отменить отложенное сохранение (например, правки отменены или сохранены вручную). */
  function cancel() {
    clearTimeout(timer)
    timer = undefined
    again = false
    if (state.value === 'pending') state.value = 'idle'
  }

  /** Остановить насовсем (документ стал недоступен для правки). */
  function stop() {
    stopped = true
    cancel()
  }

  onBeforeUnmount(() => {
    clearTimeout(timer)
  })

  return { state, error, savedAt, schedule, flush, cancel, stop }
}
