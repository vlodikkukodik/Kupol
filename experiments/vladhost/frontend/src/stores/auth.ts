import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, refreshSession, setAccessToken } from '@/api/client'
import { meSchema, sessionSchema, type User } from '@/api/schemas'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const ready = ref(false)
  const isAdmin = computed(() => user.value?.role === 'admin')

  /** Однократно при старте: пробуем поднять сессию из refresh-cookie. */
  async function init() {
    if (ready.value) return
    try {
      if (await refreshSession()) {
        user.value = (await api('/api/me', { schema: meSchema })).user
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
  }

  async function register(input: { invite: string; email: string; username: string; password: string }) {
    const s = await api('/api/auth/register', { method: 'POST', body: input, schema: sessionSchema, auth: false })
    setAccessToken(s.access_token)
    user.value = s.user
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

  return { user, ready, isAdmin, init, login, register, logout, clear }
})
