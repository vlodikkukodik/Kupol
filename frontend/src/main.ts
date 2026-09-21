import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueQueryPlugin } from '@tanstack/vue-query'

// Шрифты — свои файлы (self-hosted), кириллица + латиница. Лицензия всех: OFL-1.1.
// Интерфейс — PT Sans, заголовки и штампы — Oswald, документы — IBM Plex Mono («машинка»).
import '@fontsource/pt-sans/cyrillic-400.css'
import '@fontsource/pt-sans/latin-400.css'
import '@fontsource/pt-sans/cyrillic-700.css'
import '@fontsource/pt-sans/latin-700.css'
import '@fontsource/oswald/cyrillic-500.css'
import '@fontsource/oswald/latin-500.css'
import '@fontsource/oswald/cyrillic-700.css'
import '@fontsource/oswald/latin-700.css'
import '@fontsource/ibm-plex-mono/cyrillic-400.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/cyrillic-700.css'
import '@fontsource/ibm-plex-mono/latin-700.css'

import './styles/tokens.css'
import './styles/base.css'

import App from './App.vue'
import { createQueryClient } from './api/query'
import { i18n, onLocaleChange } from './i18n'
import { createAppRouter } from './router'
import { useAuthStore } from './stores/auth'
import { useConnectionStore } from './stores/connection'

const app = createApp(App)
const pinia = createPinia()
const queryClient = createQueryClient()
app.use(i18n)
app.use(pinia)
app.use(VueQueryPlugin, { queryClient })
app.use(createAppRouter())

// Ответы сервера (сообщения, названия уровней и статусов) приходят на языке запроса: сменили язык — запрашиваем заново
onLocaleChange(() => {
  void queryClient.invalidateQueries()
  const auth = useAuthStore(pinia)
  if (auth.user) auth.load(true).catch(() => {}) // звание в пропуске тоже приходит с сервера
})

useConnectionStore(pinia).attach()
useAuthStore(pinia).attach()

app.mount('#app')
