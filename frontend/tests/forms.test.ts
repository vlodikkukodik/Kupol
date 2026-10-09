// Общая ошибка формы: у валидации сервер шлёт общий текст («проверьте поля»), настоящая причина
// лежит в полях ответа — её и надо показать, иначе человек видит непонятное «Controlla i campi…».
import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/client'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'

describe('describeApiError', () => {
  it('валидация: текст ошибки поля важнее общего текста', () => {
    const err = new ApiError({ status: 422, code: 'validation', message: 'Проверьте поля формы', fields: { file: 'Нужна картинка JPEG, PNG или WebP либо аудио mp3 или ogg' } })
    expect(describeApiError(err)).toBe('Нужна картинка JPEG, PNG или WebP либо аудио mp3 или ogg')
  })

  it('валидация без полей — текст сервера', () => {
    const err = new ApiError({ status: 422, code: 'validation', message: 'Проверьте поля формы' })
    expect(describeApiError(err)).toBe('Проверьте поля формы')
  })

  it('коды со своим переводом поля не отменяют', () => {
    const err = new ApiError({ status: 403, code: 'forbidden', message: 'Запрос с чужого сайта отклонён', fields: { login: 'занято' } })
    expect(describeApiError(err)).toBe(t('errors.forbidden'))
  })

  it('нет ни сообщения, ни полей — общий текст', () => {
    expect(describeApiError(new ApiError({ status: 400, code: 'bad_request', message: '' }))).toBe(t('errors.generic'))
  })
})
