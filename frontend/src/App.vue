<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import { useRoute, useRouter, type RouteLocationNormalized } from 'vue-router'
import AppMenu from '@/components/AppMenu.vue'
import CookieNotice from '@/components/CookieNotice.vue'
import PassCard from '@/components/PassCard.vue'
import AuthModal from '@/components/auth/AuthModal.vue'
import UiIcon from '@/ui/UiIcon.vue'
import UiSeal from '@/ui/UiSeal.vue'
import ErrorView from '@/views/ErrorView.vue'
import MaintenanceView from '@/views/MaintenanceView.vue'
import LanguageSwitcher from '@/components/LanguageSwitcher.vue'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useConnectionStore } from '@/stores/connection'
import { useUiStore } from '@/stores/ui'
import { useConnectionMonitor } from '@/composables/useConnectionMonitor'

const route = useRoute()
const router = useRouter()
const connection = useConnectionStore()
const auth = useAuthStore()
const ui = useUiStore()
useConnectionMonitor()

// Гостя, которого охрана страниц вернула на главную с адресом возврата (?next=), встречает окно входа:
// после входа он попадёт туда, куда шёл.
function openLoginIfNeeded(to: RouteLocationNormalized) {
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

const statusText = computed(() => t(`app.status.${connection.state}`))
</script>

<template>
  <a class="skip-link" href="#content">{{ $t('app.skipLink') }}</a>

  <!-- Пока открыто меню или окно входа, страница под ними недоступна ни мыши, ни клавиатуре, ни скринридеру -->
  <div class="app" :inert="ui.overlayOpen">
    <header class="masthead">
      <button
        id="menu-toggle"
        type="button"
        class="burger"
        :aria-label="$t('app.menu')"
        aria-haspopup="dialog"
        aria-controls="site-menu"
        :aria-expanded="ui.menuOpen ? 'true' : 'false'"
        @click="ui.toggleMenu()"
      >
        <UiIcon name="menu" size="1.5rem" />
      </button>
      <RouterLink to="/" class="brand" :aria-label="$t('app.brandHome')">
        <UiSeal :size="40" decorative class="brand__seal" />
        <span class="brand__name">{{ $t('app.brandName') }}</span>
      </RouterLink>
      <nav class="masthead__nav" :aria-label="$t('app.sections')">
        <RouterLink to="/catalog">{{ $t('app.nav.catalog') }}</RouterLink>
        <RouterLink to="/search">{{ $t('app.nav.search') }}</RouterLink>
        <RouterLink to="/about">{{ $t('app.nav.about') }}</RouterLink>
      </nav>
      <LanguageSwitcher class="masthead__lang" />
      <PassCard v-if="auth.user" class="masthead__pass" :login="auth.user.login" :level-name="auth.user.level_name" :level="auth.user.level" />
    </header>

    <main id="content" tabindex="-1" class="stage">
      <MaintenanceView v-if="blocked === 'maintenance'" :retrying="connection.checking" @retry="connection.check()" />
      <ErrorView v-else-if="blocked === 'outage'" :request-id="connection.requestId" :retrying="connection.checking" @retry="connection.check()" />
      <RouterView v-else />
    </main>

    <!-- в потоке страницы, а не поверх неё: уведомление не должно перекрывать кнопки и текст -->
    <CookieNotice />

    <footer class="colophon">
      <span>{{ $t('app.footerLine') }} <RouterLink to="/about#privacy">{{ $t('app.privacy') }}</RouterLink></span>
      <span class="status" :data-state="connection.state" role="status">{{ statusText }}</span>
      <p class="disclaimer">{{ $t('app.disclaimer') }}</p>
    </footer>
  </div>

  <AppMenu />
  <AuthModal />
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  min-height: 100dvh;
}

.masthead {
  display: flex;
  align-items: center;
  flex-wrap: wrap; /* на узком экране пропуск переходит на вторую строку, а не выталкивает страницу вширь */
  gap: var(--space-2) var(--space-4);
  width: min(100% - 2 * var(--gutter), var(--page-max));
  margin-inline: auto;
  padding: var(--space-3) 0;
}
.masthead__lang {
  margin-left: auto;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--on-bg);
  text-decoration: none;
}
.brand:hover {
  color: var(--on-bg);
}
.brand__seal {
  color: var(--paper-100);
  mix-blend-mode: normal;
}
.brand__name {
  font-family: var(--font-head);
  font-size: var(--text-2xl);
  font-weight: 700;
  letter-spacing: 0.22em;
  text-transform: uppercase;
}
.masthead__nav a {
  padding: var(--space-2) var(--space-2);
  color: var(--on-bg-muted);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-decoration: none;
  text-transform: uppercase;
}
.masthead__nav a:hover,
.masthead__nav a[aria-current='page'] {
  color: var(--on-bg);
}
.masthead__nav a[aria-current='page'] {
  box-shadow: 0 2px 0 var(--red-500);
}

/* Бургер: цель нажатия не меньше 44 px */
.burger {
  display: inline-grid;
  place-items: center;
  width: var(--control-h);
  height: var(--control-h);
  padding: 0;
  border: 1.5px solid var(--line-on-bg);
  border-radius: var(--radius-2);
  background: transparent;
  color: var(--on-bg);
  cursor: pointer;
}
.burger:hover {
  border-color: var(--on-bg);
  background: rgb(255 255 255 / 0.07);
}

.stage {
  flex: 1;
  width: min(100% - 2 * var(--gutter), var(--page-max));
  margin-inline: auto;
  padding-bottom: var(--space-7);
}
.stage:focus {
  outline: none;
}

.colophon {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  width: min(100% - 2 * var(--gutter), var(--page-max));
  margin-inline: auto;
  padding: var(--space-4) 0 var(--space-5);
  border-top: 1px solid var(--line-on-bg);
  color: var(--on-bg-muted);
  font-size: var(--text-sm);
}
.disclaimer {
  flex-basis: 100%;
  margin: 0;
  font-size: var(--text-xs);
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
.status[data-state='online']::before {
  background: #8fcf8f;
  opacity: 1;
}
.status[data-state='outage']::before,
.status[data-state='maintenance']::before {
  background: #ff8a80;
  opacity: 1;
}
@media (max-width: 34rem) {
  .masthead__nav {
    display: none;
  }
}
</style>
