import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { authApi } from '@/api/endpoints'
import { api } from '@/api'
import type { UserDTO } from '@/api/generated/httpapi'

/** Права, которые присылает сервер (accounts.Capability). Интерфейс из ролей ничего не выводит — решает сервер. */
export type Capability = 'team_panel' | 'write_drafts' | 'review' | 'publish' | 'edit_published' | 'manage_glossary' | 'manage_templates' | 'manage_team'

// Загрузка сессии — один запрос на всех, кто её ждёт (роутер, главная, шапка).
let inflight: Promise<void> | null = null

export const useAuthStore = defineStore('auth', () => {
  /** Вошедший пользователь или null (Гражданин). */
  const user = ref<UserDTO | null>(null)
  /** idle — не спрашивали; loading; ready — ответ получен; failed — связи не было. */
  const status = ref<'idle' | 'loading' | 'ready' | 'failed'>('idle')
  /**
   * Резервный код, показываемый ОДИН раз (после регистрации и восстановления доступа).
   * Хранится только в памяти: на диск и в адрес не попадает.
   */
  const pendingBackupCode = ref('')
  /** Разовое сообщение для главной (например, «дело сдано в архив»). */
  const flash = ref('')

  const isAuthenticated = computed(() => user.value !== null)
  const can = (capability: Capability): boolean => Boolean(user.value?.capabilities.includes(capability))

  /** Узнать, кто вошёл. Повторные вызовы не ходят на сервер, пока force не задан. */
  function load(force = false): Promise<void> {
    if (inflight) return inflight
    if (status.value === 'ready' && !force) return Promise.resolve()
    status.value = 'loading'
    inflight = authApi
      .session()
      .then((res) => {
        user.value = res.user ?? null
        status.value = 'ready'
      })
      .catch((err: unknown) => {
        status.value = 'failed'
        throw err
      })
      .finally(() => {
        inflight = null
      })
    return inflight
  }

  async function login(loginName: string, password: string) {
    const res = await authApi.login({ login: loginName, password })
    user.value = res.user
    status.value = 'ready'
  }

  async function register(p: { login: string; password: string; captchaId: string; captchaAnswer: string }) {
    const res = await authApi.register({ login: p.login, password: p.password, captcha_id: p.captchaId, captcha_answer: p.captchaAnswer })
    user.value = res.user
    status.value = 'ready'
    pendingBackupCode.value = res.backup_code
  }

  async function restore(p: { login: string; backupCode: string; newPassword: string }) {
    const res = await authApi.restore({ login: p.login, backup_code: p.backupCode, new_password: p.newPassword })
    user.value = res.user
    status.value = 'ready'
    pendingBackupCode.value = res.backup_code
  }

  /** Выход. Локальное состояние сбрасывается только после подтверждения сервером. */
  async function logout() {
    await authApi.logout()
    user.value = null
  }

  async function changePassword(currentPassword: string, newPassword: string) {
    await authApi.changePassword({ current_password: currentPassword, new_password: newPassword })
  }

  /** «Сдать дело в архив»: удалить аккаунт. */
  async function deleteAccount(password: string) {
    await authApi.deleteAccount(password)
    user.value = null
    flash.value = 'Дело сдано в архив. Аккаунт и связанные с ним данные удалены.'
  }

  function acknowledgeBackupCode() {
    pendingBackupCode.value = ''
  }

  function takeFlash(): string {
    const msg = flash.value
    flash.value = ''
    return msg
  }

  /** Подписка на исходы всех запросов: сервер сказал «не вошли» — забываем пользователя. */
  function attach(client = api) {
    return client.subscribe((event) => {
      if (!event.ok && event.error.code === 'unauthenticated') user.value = null
    })
  }

  return { user, status, pendingBackupCode, flash, isAuthenticated, can, load, login, register, restore, logout, changePassword, deleteAccount, acknowledgeBackupCode, takeFlash, attach }
})
