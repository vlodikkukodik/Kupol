<script setup>
import { computed, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthModal from './components/AuthModal.vue'
import PassCard from './components/PassCard.vue'
import SealMark from './components/SealMark.vue'
import SideMenu from './components/SideMenu.vue'
import ErrorView from './views/ErrorView.vue'
import MaintenanceView from './views/MaintenanceView.vue'
import { useAuthStore } from './stores/auth.js'
import { useConnectionStore } from './stores/connection.js'
import { useUiStore } from './stores/ui.js'
import { useConnectionMonitor } from './composables/useConnectionMonitor.js'

const route = useRoute()
const router = useRouter()
const connection = useConnectionStore()
const auth = useAuthStore()
const ui = useUiStore()
useConnectionMonitor()

// Гостя, которого охрана страниц вернула на главную с адресом возврата (?next=), встречает окно входа:
// после входа он попадёт туда, куда шёл.
function openLoginIfNeeded(to) {
  if (to.query.next && auth.status === 'ready' && !auth.user) ui.openAuth()
}

// Переход на другую страницу закрывает меню и окно входа. Завершение первой загрузки (from без маршрута) — не переход:
// читатель мог успеть открыть меню, пока страница подгружалась, и оно должно остаться открытым.
const removeAfterEach = router.afterEach((to, from, failure) => {
  if (failure) return
  if (from.matched.length > 0) ui.closeAll()
  openLoginIfNeeded(to)
})
onBeforeUnmount(removeAfterEach)
if (router.currentRoute.value.matched.length > 0) openLoginIfNeeded(router.currentRoute.value)

// Закрытый архив закрывает весь сайт; сбой связи — только страницы, которым нужен API.
const blocked = computed(() => {
  if (connection.state === 'maintenance') return 'maintenance'
  if (connection.state === 'outage' && route.meta.needsApi) return 'outage'
  return null
})

const statusText = computed(
  () =>
    ({
      unknown: 'Связь с архивом: проверка…',
      online: 'Связь с архивом: установлена',
      outage: 'Связь с архивом: нарушена',
      maintenance: 'Архив закрыт на инвентаризацию',
    })[connection.state],
)
</script>

<template>
  <a class="skip-link" href="#content">К содержимому</a>

  <!-- Пока открыто меню или окно входа, страница под ними недоступна ни мыши, ни клавиатуре, ни скринридеру -->
  <div class="page" :inert="ui.overlayOpen ? '' : null">
    <header class="masthead">
      <button
        id="menu-toggle"
        type="button"
        class="burger"
        aria-label="Меню"
        aria-haspopup="dialog"
        aria-controls="site-menu"
        :aria-expanded="ui.menuOpen ? 'true' : 'false'"
        @click="ui.toggleMenu()"
      >
        <span class="burger-bars" aria-hidden="true" />
      </button>
      <RouterLink to="/" class="brand" aria-label="КУПОЛ — на главную">
        <SealMark :size="40" decorative />
        <span class="brand-name">Купол</span>
      </RouterLink>
      <PassCard v-if="auth.user" :login="auth.user.login" :level-name="auth.user.level_name" />
    </header>

    <main id="content" class="sheet" tabindex="-1">
      <MaintenanceView v-if="blocked === 'maintenance'" :retrying="connection.checking" @retry="connection.check()" />
      <ErrorView
        v-else-if="blocked === 'outage'"
        :request-id="connection.requestId"
        :retrying="connection.checking"
        @retry="connection.check()"
      />
      <RouterView v-else />
    </main>

    <footer class="colophon">
      <span>Форма КУПОЛ-1 · Центральный архив</span>
      <span class="status" :data-state="connection.state" role="status">{{ statusText }}</span>
      <p class="disclaimer">
        Примечание автора: КУПОЛ, его объекты, сотрудники и события — художественный вымысел. Любые совпадения
        с реальными организациями, документами и людьми случайны. Это литературно-игровой архив, а не источник фактов.
      </p>
    </footer>
  </div>

  <SideMenu />
  <AuthModal />
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  min-height: 100dvh;
  width: min(100% - 2 * var(--space-3), var(--sheet-width));
  margin-inline: auto;
}

.masthead {
  display: flex;
  align-items: center;
  flex-wrap: wrap; /* на узком экране пропуск переходит на вторую строку, а не выталкивает страницу вширь */
  gap: var(--space-2) var(--space-3);
  padding: var(--space-3) 0;
}
.masthead :deep(.pass) { margin-left: auto; }
.brand {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--paper);
  text-decoration: none;
}
.brand :deep(.seal) { color: var(--paper); mix-blend-mode: normal; }
.brand-name {
  font-family: var(--font-head);
  font-weight: 700;
  font-size: 1.4rem;
  letter-spacing: 0.2em;
  text-transform: uppercase;
}

/* Бургер: три полоски, цель нажатия не меньше 44 px */
.burger {
  display: inline-grid;
  place-items: center;
  width: 2.75rem;
  height: 2.75rem;
  padding: 0;
  border: 2px solid var(--paper);
  border-radius: var(--radius);
  background: transparent;
  color: var(--paper);
  cursor: pointer;
}
.burger:hover { background: var(--paper); color: var(--desk); }
.burger-bars,
.burger-bars::before,
.burger-bars::after {
  display: block;
  width: 1.3rem;
  height: 2px;
  background: currentColor;
}
.burger-bars { position: relative; }
.burger-bars::before, .burger-bars::after { content: ''; position: absolute; left: 0; }
.burger-bars::before { top: -0.4rem; }
.burger-bars::after { top: 0.4rem; }

.sheet {
  flex: 1;
  padding: var(--space-5) var(--space-5) var(--space-6);
  background:
    linear-gradient(180deg, rgb(255 255 255 / 0.18), transparent 12rem),
    var(--paper);
  border-radius: var(--radius);
  box-shadow: var(--shadow-sheet);
}
.sheet:focus { outline: none; }

.colophon {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  padding: var(--space-3) 0 var(--space-4);
  color: var(--paper);
  font-size: 0.85rem;
  opacity: 0.9;
}
.disclaimer {
  flex-basis: 100%;
  margin: 0;
  padding-top: var(--space-2);
  border-top: 1px solid rgb(233 224 200 / 0.3);
  font-size: 0.78rem;
}
.status::before {
  content: '';
  display: inline-block;
  width: 0.6em;
  height: 0.6em;
  margin-right: 0.5em;
  border-radius: 50%;
  background: currentColor;
  opacity: 0.5;
}
.status[data-state='online']::before { background: #8fcf8f; opacity: 1; }
.status[data-state='outage']::before,
.status[data-state='maintenance']::before { background: #ff8a80; opacity: 1; }

@media (max-width: 34rem) {
  .sheet { padding: var(--space-4) var(--space-3) var(--space-5); }
}
</style>
