import { createPinia } from 'pinia'
import { createApp } from 'vue'
import { onUnauthorized } from '@/api/client'
import { router } from '@/router'
import { useAuthStore } from '@/stores/auth'
import App from './App.vue'

const app = createApp(App)
app.use(createPinia())
app.use(router)

// Сессия истекла посреди работы — возвращаем на вход.
onUnauthorized(() => {
  useAuthStore().clear()
  if (router.currentRoute.value.meta.auth) {
    void router.push({ name: 'login', query: { next: router.currentRoute.value.fullPath } })
  }
})

app.mount('#app')
