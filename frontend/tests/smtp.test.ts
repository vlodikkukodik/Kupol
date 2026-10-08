// Панель команды → Почтовый сервер: настройки читает вся команда, правит только Директорат,
// а пустое поле пароля не должно затирать сохранённый — ключ в запрос не попадает вовсе.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import TeamSmtpView from '@/views/team/TeamSmtpView.vue'
import { teamApi } from '@/api/endpoints'
import type * as endpoints from '@/api/endpoints'
import { t } from '@/i18n'
import type { Input as SmtpInput, Out } from '@/api/generated/mailsettings'

vi.mock('@/api/endpoints', async (importOriginal) => {
  const actual = await importOriginal<typeof endpoints>()
  return { ...actual, teamApi: { ...actual.teamApi, smtp: vi.fn(), updateSmtp: vi.fn(), testSmtp: vi.fn() } }
})

const smtp = vi.mocked(teamApi.smtp)
const updateSmtp = vi.mocked(teamApi.updateSmtp)
const testSmtp = vi.mocked(teamApi.testSmtp)

const settings = (patch: Partial<Out> = {}): Out => ({
  enabled: true,
  host: 'smtp.example.org',
  port: '587',
  username: 'postmaster@example.org',
  password_set: true,
  from: 'no-reply@example.org',
  from_name: 'КУПОЛ',
  source: 'env',
  can_edit: true,
  ...patch,
})

let pinia: Pinia

async function open(settingsOut: Out = settings()): Promise<VueWrapper> {
  setActivePinia(pinia)
  smtp.mockResolvedValue(settingsOut)
  updateSmtp.mockResolvedValue(settingsOut)
  testSmtp.mockResolvedValue(null)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const w = mount(TeamSmtpView, { global: { plugins: [pinia, [VueQueryPlugin, { queryClient }]] } })
  await flushPromises()
  return w
}

const field = (w: VueWrapper, id: string) => w.find(`[data-testid="${id}"] input`)
const value = (w: VueWrapper, id: string) => (field(w, id).element as HTMLInputElement).value

/** Что ушло на сервер: тело первого сохранения. */
const sentBody = (): SmtpInput => {
  const call = updateSmtp.mock.calls[0]
  if (!call) throw new Error('updateSmtp не вызван')
  return call[0]
}

beforeEach(() => {
  vi.clearAllMocks()
  pinia = createPinia()
})

describe('TeamSmtpView', () => {
  it('показывает настройки и происхождение, а членам команды без Директората — только чтение', async () => {
    const w = await open()
    expect(value(w, 'smtp-host')).toBe('smtp.example.org')
    expect(w.find('[data-testid="smtp-source"]').text()).toBe(t('smtp.sourceEnv'))
    expect(w.find('[data-testid="smtp-save"]').exists()).toBe(true)

    const guest = await open(settings({ can_edit: false }))
    expect(guest.find('[data-testid="smtp-readonly"]').exists()).toBe(true)
    expect(guest.find('[data-testid="smtp-save"]').exists()).toBe(false)
    expect(field(guest, 'smtp-host').attributes('disabled')).toBeDefined()
  })

  it('пустое поле пароля не уходит на сервер — сохранённый остаётся', async () => {
    const w = await open()
    expect(w.find('[data-testid="smtp-save"]').attributes('disabled')).toBeDefined()

    await field(w, 'smtp-host').setValue('smtp.other.example.org')
    expect(w.find('[data-testid="smtp-save"]').attributes('disabled')).toBeUndefined()
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(updateSmtp).toHaveBeenCalledTimes(1)
    const body = sentBody()
    expect(body).not.toHaveProperty('password')
    expect(body).toMatchObject({ enabled: true, host: 'smtp.other.example.org', username: 'postmaster@example.org' })
    expect(w.find('[data-testid="smtp-notice"]').exists()).toBe(true)
  })

  it('набранный пароль уходит в запрос, а после сохранения поле снова пустое', async () => {
    const w = await open()
    await field(w, 'smtp-password').setValue('новый пароль')
    await w.find('form').trigger('submit')
    await flushPromises()

    expect(sentBody().password).toBe('новый пароль')
    expect(value(w, 'smtp-password')).toBe('')
  })

  it('письмо-проверка не отправляется, пока почта выключена', async () => {
    const w = await open(settings({ enabled: false }))
    expect(w.find('[data-testid="smtp-test-send"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain(t('smtp.testDisabled'))
    expect(testSmtp).not.toHaveBeenCalled()
  })

  it('письмо-проверка уходит с введённым адресом', async () => {
    const w = await open()
    await field(w, 'smtp-test-to').setValue('reader@example.org')
    await w.find('[data-testid="smtp-test-send"]').trigger('click')
    await flushPromises()

    expect(testSmtp).toHaveBeenCalledWith('reader@example.org')
    expect(w.find('[data-testid="smtp-test-notice"]').exists()).toBe(true)
  })
})
