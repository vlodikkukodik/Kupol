import { reactive, ref } from 'vue'
import { ApiError } from '@/api/client'
import { formatWait } from '@/lib/format'
import { t } from '@/i18n'

/** Сообщение для общей ошибки формы (не привязанной к полю) по коду ответа API. */
export function describeApiError(err: ApiError): string {
  switch (err.code) {
    case 'rate_limited':
      return t('errors.rateLimited', { wait: formatWait(err.retryAfter) })
    case 'network':
    case 'timeout':
      return t('errors.network')
    case 'forbidden_origin':
      return t('errors.forbiddenOrigin')
    case 'forbidden':
      return t('errors.forbidden')
    case 'locked':
      return err.lock ? t('errors.lockedBy', { holder: err.lock.holder }) : t('errors.locked')
    case 'conflict':
      return t('errors.conflict')
    case 'self_review':
      return t('errors.selfReview')
    case 'invalid_state':
      return t('errors.invalidState')
    case 'lint_failed':
      return err.message || t('errors.lintFailed')
    case 'internal':
    case 'upstream_unavailable':
    case 'upstream_timeout':
    case 'proxy_misconfigured':
      return t('errors.internal')
    default:
      return err.message || t('errors.generic')
  }
}

/**
 * Состояние формы: ошибки по полям, общая ошибка, признак отправки.
 * Ошибки полей и общая ошибка озвучиваются скринридерами (role="alert" в шаблонах).
 */
export function useForm() {
  const errors = reactive<Record<string, string>>({})
  const formError = ref('')
  const submitting = ref(false)
  /** Последняя ошибка API (для решений вроде «обновить вопрос анкеты»); null после успеха. */
  const lastError = ref<ApiError | null>(null)

  function clear() {
    for (const k of Object.keys(errors)) delete errors[k]
    formError.value = ''
    lastError.value = null
  }

  /** Ошибки полей из ответа сервера; имена полей можно переименовать через fieldMap. */
  function applyApiError(err: ApiError, fieldMap: Record<string, string> = {}): number {
    let assigned = 0
    for (const [name, message] of Object.entries(err.fields)) {
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
  async function submit(fn: () => Promise<unknown>, fieldMap?: Record<string, string>): Promise<boolean> {
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
