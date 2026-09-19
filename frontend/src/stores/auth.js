import { defineStore } from 'pinia'
import { api } from '../api/index.js'

// Загрузка сессии — один запрос на всех, кто её ждёт (роутер, главная, шапка).
let inflight = null

export const useAuthStore = defineStore('auth', {
  state: () => ({
    /** Вошедший пользователь или null (Гражданин). */
    user: null,
    /** idle — не спрашивали; loading; ready — ответ получен; failed — связи не было. */
    status: 'idle',
    /**
     * Резервный код, показываемый ОДИН раз (после регистрации и восстановления доступа).
     * Хранится только в памяти: на диск и в адрес не попадает.
     */
    pendingBackupCode: '',
    /** Разовое сообщение для главной (например, «дело сдано в архив»). */
    flash: '',
  }),

  getters: {
    isAuthenticated: (s) => s.user !== null,
    /** Есть ли право (например, 'manage_team'). Список прав присылает сервер — интерфейс из ролей ничего не выводит. */
    can: (s) => (capability) => Boolean(s.user?.capabilities?.includes(capability)),
  },

  actions: {
    /** Узнать, кто вошёл. Повторные вызовы не ходят на сервер, пока force не задан. */
    async load(force = false) {
      if (inflight) return inflight
      if (this.status === 'ready' && !force) return undefined
      this.status = 'loading'
      inflight = api
        .get('/auth/session')
        .then((res) => {
          this.user = res.user
          this.status = 'ready'
        })
        .catch((err) => {
          this.status = 'failed'
          throw err
        })
        .finally(() => {
          inflight = null
        })
      return inflight
    },

    async login(login, password) {
      const res = await api.post('/auth/login', { login, password })
      this.user = res.user
      this.status = 'ready'
    },

    async register({ login, password, captchaId, captchaAnswer }) {
      const res = await api.post('/auth/register', {
        login,
        password,
        captcha_id: captchaId,
        captcha_answer: captchaAnswer,
      })
      this.user = res.user
      this.status = 'ready'
      this.pendingBackupCode = res.backup_code
    },

    async restore({ login, backupCode, newPassword }) {
      const res = await api.post('/auth/restore', { login, backup_code: backupCode, new_password: newPassword })
      this.user = res.user
      this.status = 'ready'
      this.pendingBackupCode = res.backup_code
    },

    /** Выход. Локальное состояние сбрасывается только после подтверждения сервером. */
    async logout() {
      await api.post('/auth/logout')
      this.user = null
    },

    async changePassword(currentPassword, newPassword) {
      await api.post('/me/password', { current_password: currentPassword, new_password: newPassword })
    },

    /** «Сдать дело в архив»: удалить аккаунт. */
    async deleteAccount(password) {
      await api.delete('/me', { password })
      this.user = null
      this.flash = 'Дело сдано в архив. Аккаунт и связанные с ним данные удалены.'
    },

    acknowledgeBackupCode() {
      this.pendingBackupCode = ''
    },

    takeFlash() {
      const msg = this.flash
      this.flash = ''
      return msg
    },

    /** Подписка на исходы всех запросов: сервер сказал «не вошли» — забываем пользователя. */
    attach(client = api) {
      return client.subscribe((event) => {
        if (!event.ok && event.error.code === 'unauthenticated') this.user = null
      })
    },
  },
})
