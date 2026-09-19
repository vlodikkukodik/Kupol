import { reactive, ref } from 'vue'
import { ApiError } from '../api/client.js'
import { formatWait } from '../lib/format.js'

/** Сообщение для общей ошибки формы (не привязанной к полю) по коду ответа API. */
export function describeApiError(err) {
  switch (err.code) {
    case 'rate_limited':
      return `Слишком много попыток. Повторите через ${formatWait(err.retryAfter)}.`
    case 'network':
    case 'timeout':
      return 'Нет связи с архивом. Проверьте подключение и повторите.'
    case 'forbidden_origin':
      return 'Запрос отклонён. Откройте сайт по обычному адресу и повторите.'
    case 'forbidden':
      return 'Недостаточно прав для этого действия.'
    case 'locked':
      return err.lock ? `Документ сейчас правит ${err.lock.holder}.` : 'Документ сейчас правит другой сотрудник.'
    case 'conflict':
      return 'Документ изменён после того, как вы его открыли.'
    case 'internal':
    case 'upstream_unavailable':
    case 'upstream_timeout':
    case 'proxy_misconfigured':
      return 'Сбой архива. Повторите попытку позже.'
    default:
      return err.message || 'Не удалось выполнить запрос.'
  }
}

/**
 * Состояние формы: ошибки по полям, общая ошибка, признак отправки.
 * Ошибки полей и общая ошибка озвучиваются скринридерами (role="alert" в шаблонах).
 */
export function useForm() {
  const errors = reactive({})
  const formError = ref('')
  const submitting = ref(false)
  /** Последняя ошибка API (для решений вроде «обновить вопрос анкеты»); null после успеха. */
  const lastError = ref(null)

  function clear() {
    for (const k of Object.keys(errors)) delete errors[k]
    formError.value = ''
    lastError.value = null
  }

  /** Ошибки полей из ответа сервера; имена полей можно переименовать через fieldMap. */
  function applyApiError(err, fieldMap = {}) {
    let assigned = 0
    for (const [name, message] of Object.entries(err.fields || {})) {
      errors[fieldMap[name] || name] = message
      assigned++
    }
    // Ошибки валидации без привязки к полю и все прочие показываем общим сообщением
    if (assigned === 0 || err.code === 'rate_limited') formError.value = describeApiError(err)
    return assigned
  }

  /**
   * Выполняет отправку: сбрасывает прежние ошибки, блокирует повторную отправку и раскладывает
   * ApiError по полям. Прочие исключения (ошибки программы) не глотаются.
   */
  async function submit(fn, fieldMap) {
    if (submitting.value) return false
    clear()
    submitting.value = true
    try {
      await fn()
      return true
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
      lastError.value = err
      applyApiError(err, fieldMap)
      return false
    } finally {
      submitting.value = false
    }
  }

  return { errors, formError, submitting, lastError, clear, submit, applyApiError }
}
