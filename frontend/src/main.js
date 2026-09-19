import { createApp } from 'vue'
import { createPinia } from 'pinia'

// Шрифты — свои файлы (self-hosted), кириллица + латиница. Лицензия обоих: OFL-1.1.
import '@fontsource/pt-mono/cyrillic-400.css'
import '@fontsource/pt-mono/latin-400.css'
import '@fontsource/oswald/cyrillic-500.css'
import '@fontsource/oswald/latin-500.css'
import '@fontsource/oswald/cyrillic-700.css'
import '@fontsource/oswald/latin-700.css'

import './styles/tokens.css'
import './styles/base.css'
import './styles/forms.css'

import App from './App.vue'
import { createAppRouter } from './router/index.js'
import { useAuthStore } from './stores/auth.js'
import { useConnectionStore } from './stores/connection.js'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(createAppRouter())

useConnectionStore(pinia).attach()
useAuthStore(pinia).attach()

app.mount('#app')
