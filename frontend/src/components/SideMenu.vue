<script setup>
import { ref } from 'vue'
import SealMark from './SealMark.vue'
import { useOverlay } from '../composables/useOverlay.js'
import { useAuthStore } from '../stores/auth.js'
import { useUiStore } from '../stores/ui.js'

const ui = useUiStore()
const auth = useAuthStore()
const panel = ref(null)

useOverlay({
  active: () => ui.menuOpen,
  container: panel,
  onClose: () => ui.closeMenu(),
  fallbackFocus: () => document.getElementById('menu-toggle'),
})
</script>

<template>
  <Teleport to="body">
    <Transition name="drawer">
      <div v-if="ui.menuOpen" class="drawer-root">
        <div class="scrim" data-testid="menu-scrim" @click="ui.closeMenu()" />
        <aside id="site-menu" ref="panel" class="drawer" role="dialog" aria-modal="true" aria-label="Меню" tabindex="-1">
          <div class="drawer-head">
            <RouterLink to="/" class="drawer-brand" @click="ui.closeMenu()">
              <SealMark :size="36" decorative />
              <span class="drawer-brand-name">Купол</span>
            </RouterLink>
            <button type="button" class="drawer-close" aria-label="Закрыть меню" @click="ui.closeMenu()">
              <span aria-hidden="true">×</span>
            </button>
          </div>

          <nav class="drawer-nav" aria-label="Основная навигация">
            <ul>
              <li><RouterLink to="/" @click="ui.closeMenu()">Главная</RouterLink></li>
              <li><RouterLink to="/catalog" @click="ui.closeMenu()">Каталог</RouterLink></li>
              <li v-if="auth.user"><RouterLink to="/file" @click="ui.closeMenu()">Личное дело</RouterLink></li>
              <li v-if="auth.can('team_panel')"><RouterLink to="/team" @click="ui.closeMenu()">Панель команды</RouterLink></li>
            </ul>
          </nav>

          <section class="drawer-account" aria-labelledby="menu-account-title">
            <h2 id="menu-account-title">Допуск</h2>
            <template v-if="auth.user">
              <p class="who">
                Вы вошли как <strong>{{ auth.user.login }}</strong><br>
                {{ auth.user.level_name }}
              </p>
            </template>
            <template v-else>
              <p class="who">Вы не вошли: уровень 0, Гражданин.</p>
              <button type="button" class="btn drawer-login" @click="ui.openAuth()">Войти или зарегистрироваться</button>
            </template>
          </section>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.drawer-root { position: fixed; inset: 0; z-index: 50; }
.scrim { position: absolute; inset: 0; background: rgb(0 0 0 / 0.6); }

.drawer {
  position: absolute;
  inset: 0 auto 0 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  width: min(20rem, 88vw);
  padding: var(--space-3) var(--space-4) var(--space-4);
  overflow-y: auto;
  background: var(--paper);
  color: var(--ink);
  box-shadow: 0 0 32px rgb(0 0 0 / 0.6);
  border-right: 2px solid var(--ink);
}
.drawer:focus { outline: none; }

.drawer-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); }
.drawer-brand { display: inline-flex; align-items: center; gap: var(--space-2); color: var(--ink); text-decoration: none; }
.drawer-brand :deep(.seal) { color: var(--stamp-red); }
.drawer-brand-name {
  font-family: var(--font-head);
  font-size: 1.3rem;
  font-weight: 700;
  letter-spacing: 0.2em;
  text-transform: uppercase;
}
.drawer-close {
  width: 2.75rem;
  height: 2.75rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink);
  font-size: 1.8rem;
  line-height: 1;
  cursor: pointer;
}
.drawer-close:hover { background: var(--ink); color: var(--paper); }

.drawer-nav ul { margin: 0; padding: 0; list-style: none; border-top: 2px solid var(--ink); }
.drawer-nav li { border-bottom: 1px solid var(--rule); }
.drawer-nav a {
  display: block;
  padding: 0.75rem 0.7rem;
  color: var(--ink);
  font-family: var(--font-head);
  font-size: 1.15rem;
  letter-spacing: 0.12em;
  text-decoration: none;
  text-transform: uppercase;
}
.drawer-nav a:hover { background: var(--paper-shade); }
.drawer-nav a[aria-current='page'] { background: var(--ink); color: var(--paper); }

.drawer-account { margin-top: auto; padding-top: var(--space-3); border-top: 2px solid var(--ink); }
.drawer-account h2 { margin-bottom: var(--space-2); font-size: 1rem; letter-spacing: 0.14em; }
.who { margin: 0 0 var(--space-3); overflow-wrap: anywhere; }
.drawer-login { width: 100%; }

/* Выезд слева; при prefers-reduced-motion общая настройка сводит длительность к нулю. */
.drawer-enter-active, .drawer-leave-active { transition: opacity 0.2s ease; }
.drawer-enter-active .drawer, .drawer-leave-active .drawer { transition: transform 0.22s ease; }
.drawer-enter-from, .drawer-leave-to { opacity: 0; }
.drawer-enter-from .drawer, .drawer-leave-to .drawer { transform: translateX(-100%); }
</style>
