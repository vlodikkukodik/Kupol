// Письма-уведомления в личном деле: читатель переключает виды записок, которые хочет получать
// на почту, — сохраняется сразу, при сбое прежнее значение возвращается.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import NotifyPanel from '@/components/NotifyPanel.vue'
import { authApi } from '@/api/endpoints'
import type * as endpoints from '@/api/endpoints'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { t } from '@/i18n'
import type { EmailPrefs } from '@/api/generated/inbox'
import type { UserDTO } from '@/api/generated/httpapi'

vi.mock('@/api/endpoints', async (importOriginal) => {
  const actual = await importOriginal<typeof endpoints>()
  return { ...actual, authApi: { ...actual.authApi, emailPrefs: vi.fn(), setEmailPrefs: vi.fn() } }
})

const all = (patch: Partial<EmailPrefs> = {}): EmailPrefs => ({
  enabled: true,
  note: true,
  level_up: true,
  achievement: true,
  suggestion: true,
  remark_reply: true,
  petition: true,
  invitation: true,
  invitation_answer: true,
  sanction: true,
  ...patch,
})

const emailPrefs = vi.mocked(authApi.emailPrefs)
const setEmailPrefs = vi.mocked(authApi.setEmailPrefs)

let pinia: Pinia

const reader: UserDTO = {
  login: 'читатель',
  level: 2,
  level_name: 'Посетитель',
  directorate: false,
  created_at: '2026-09-19T12:00:00Z',
  roles: [],
  capabilities: [],
  totp_enabled: false,
  xp: 10,
  login_streak: 1,
  next_level_xp: 100,
  lang: 'ru',
  email: 'reader@example.org',
}

async function openPanel(user: UserDTO | null = reader): Promise<VueWrapper> {
  setActivePinia(pinia)
  useAuthStore().user = user
  const w = mount(NotifyPanel, { global: { plugins: [pinia] } })
  await flushPromises()
  return w
}

const box = (w: VueWrapper, id: string) => w.find(`[data-testid="${id}"] input`)
const flag = (w: VueWrapper, id: string, key: 'checked' | 'disabled') => (box(w, id).element as HTMLInputElement)[key]

/** Что ушло на сервер: первый вызов сохранения. */
const sentPrefs = (): EmailPrefs => {
  const call = setEmailPrefs.mock.calls[0]
  if (!call) throw new Error('setEmailPrefs не вызван')
  return call[0]
}

beforeEach(() => {
  vi.clearAllMocks()
  pinia = createPinia()
  emailPrefs.mockResolvedValue(all())
  setEmailPrefs.mockResolvedValue(null)
})

describe('NotifyPanel', () => {
  it('показывает общий тумблер и девять видов записок', async () => {
    const w = await openPanel()
    expect(flag(w, 'notify-enabled', 'checked')).toBe(true)
    expect(w.find('[data-testid="notify-kinds"]').findAll('li')).toHaveLength(9)
    expect(w.find('[data-testid="notify-kind-sanction"] label').text()).toBe(t('notify.kinds.sanction'))
    expect(w.find('[data-testid="notify-saving"]').exists()).toBe(false)
    expect(w.find('[data-testid="notify-error"]').exists()).toBe(false)
  })

  it('переключение вида уходит на сервер тем же объектом, но с новым значением', async () => {
    const w = await openPanel()
    let release!: () => void
    setEmailPrefs.mockReturnValue(new Promise<null>((r) => (release = () => r(null))))

    await box(w, 'notify-kind-note').setValue(false)
    await flushPromises()
    expect(w.find('[data-testid="notify-saving"]').exists()).toBe(true)
    expect(setEmailPrefs).toHaveBeenCalledTimes(1)
    expect(sentPrefs()).toMatchObject({ enabled: true, note: false, level_up: true })

    release()
    await flushPromises()
    expect(w.find('[data-testid="notify-saving"]').exists()).toBe(false)
    expect(w.find('[data-testid="notify-error"]').exists()).toBe(false)
    expect(flag(w, 'notify-kind-note', 'checked')).toBe(false)
  })

  it('при сбое сервера прежнее значение возвращается, а ошибка показывается', async () => {
    setEmailPrefs.mockRejectedValue(new ApiError({ status: 500, code: 'internal', message: 'Сбой архива' }))
    const w = await openPanel()

    await box(w, 'notify-kind-achievement').setValue(false)
    await flushPromises()

    expect(flag(w, 'notify-kind-achievement', 'checked')).toBe(true)
    expect(w.find('[data-testid="notify-error"]').exists()).toBe(true)
  })

  it('общий тумблер выключает все письма разом и гасит виды', async () => {
    const w = await openPanel()

    await box(w, 'notify-enabled').setValue(false)
    await flushPromises()

    expect(sentPrefs()).toMatchObject({ enabled: false })
    expect(w.find('[data-testid="notify-off"]').exists()).toBe(true)
    expect(flag(w, 'notify-kind-note', 'disabled')).toBe(true)
  })

  it('без подтверждённой почты — подсказка, а не обещание писем', async () => {
    const w = await openPanel({ ...reader, email: undefined })
    expect(w.find('[data-testid="notify-noemail"]').exists()).toBe(true)

    const withMail = await openPanel(reader)
    expect(withMail.find('[data-testid="notify-noemail"]').exists()).toBe(false)
  })
})
