import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, refreshSession, setAccessToken } from '@/api/client'
import { meSchema, sessionSchema, type User } from '@/api/schemas'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const mailEnabled = ref(false)
  const databasesEnabled = ref(false)
  const shellEnabled = ref(false)
  const mailhostEnabled = ref(false)
  const dnsEnabled = ref(false)
  const ready = ref(false)
  const isAdmin = computed(() => user.value?.role === 'admin')

  /** Однократно при старте: пробуем поднять сессию из refresh-cookie. */
  async function init() {
    if (ready.value) return
    try {
      if (await refreshSession()) {
        const me = await api('/api/me', { schema: meSchema })
        user.value = me.user
        mailEnabled.value = me.mail_enabled
    databasesEnabled.value = me.databases_enabled
    shellEnabled.value = me.shell_enabled
    mailhostEnabled.value = me.mailhost_enabled
    dnsEnabled.value = me.dns_enabled
      }
    } catch {
      user.value = null
    } finally {
      ready.value = true
    }
  }

  async function login(login: string, password: string) {
    const s = await api('/api/auth/login', { method: 'POST', body: { login, password }, schema: sessionSchema, auth: false })
    setAccessToken(s.access_token)
    user.value = s.user
    mailEnabled.value = s.mail_enabled
    databasesEnabled.value = s.databases_enabled
    shellEnabled.value = s.shell_enabled
    mailhostEnabled.value = s.mailhost_enabled
    dnsEnabled.value = s.dns_enabled
  }

  async function register(input: { invite: string; email: string; username: string; password: string }) {
    const s = await api('/api/auth/register', { method: 'POST', body: input, schema: sessionSchema, auth: false })
    setAccessToken(s.access_token)
    user.value = s.user
    mailEnabled.value = s.mail_enabled
    databasesEnabled.value = s.databases_enabled
    shellEnabled.value = s.shell_enabled
    mailhostEnabled.value = s.mailhost_enabled
    dnsEnabled.value = s.dns_enabled
  }

  /** Смена пароля: сервер закрывает остальные сессии и выдаёт новую для этого устройства. */
  async function changePassword(currentPassword: string, newPassword: string) {
    const s = await api('/api/me/password', {
      method: 'POST',
      body: { current_password: currentPassword, new_password: newPassword },
      schema: sessionSchema,
    })
    setAccessToken(s.access_token)
    user.value = s.user
    mailEnabled.value = s.mail_enabled
    databasesEnabled.value = s.databases_enabled
    shellEnabled.value = s.shell_enabled
    mailhostEnabled.value = s.mailhost_enabled
    dnsEnabled.value = s.dns_enabled
  }

  /** Письмо с подтверждением адреса ещё раз. */
  async function resendVerification() {
    await api('/api/me/email/verify', { method: 'POST' })
  }

  /** Язык писем и согласие на уведомления. */
  async function updatePreferences(p: { lang?: 'ru' | 'it'; notify_email?: boolean }) {
    const r = await api('/api/me', { method: 'PATCH', body: p, schema: meSchema })
    user.value = r.user
    mailEnabled.value = r.mail_enabled
    databasesEnabled.value = r.databases_enabled
    shellEnabled.value = r.shell_enabled
    mailhostEnabled.value = r.mailhost_enabled
    dnsEnabled.value = r.dns_enabled
  }

  /** Адрес подтверждён (по ссылке из письма в этой же вкладке): обновляем данные, не перезагружая страницу. */
  async function refreshMe() {
    const me = await api('/api/me', { schema: meSchema })
    user.value = me.user
    mailEnabled.value = me.mail_enabled
    databasesEnabled.value = me.databases_enabled
    shellEnabled.value = me.shell_enabled
    mailhostEnabled.value = me.mailhost_enabled
    dnsEnabled.value = me.dns_enabled
  }

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST', auth: false })
    } finally {
      clear()
    }
  }

  function clear() {
    setAccessToken(null)
    user.value = null
  }

  return { user, mailEnabled, databasesEnabled, shellEnabled, mailhostEnabled, dnsEnabled, ready, isAdmin, init, login, register, changePassword, resendVerification, updatePreferences, refreshMe, logout, clear }
})
