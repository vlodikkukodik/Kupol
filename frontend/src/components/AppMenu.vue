<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiDrawer from '@/ui/UiDrawer.vue'
import UiSeal from '@/ui/UiSeal.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

// Боковое меню: разделы архива и сведения о допуске читателя.
const ui = useUiStore()
const auth = useAuthStore()
const toggle = () => document.getElementById('menu-toggle')
</script>

<template>
  <UiDrawer
    id="site-menu"
    :open="ui.menuOpen"
    label="Меню"
    close-label="Закрыть меню"
    scrim-testid="menu-scrim"
    :fallback-focus="toggle"
    @update:open="(v: boolean) => !v && ui.closeMenu()"
  >
    <template #head>
      <RouterLink to="/" class="brand" @click="ui.closeMenu()">
        <UiSeal :size="38" decorative />
        <span class="brand__name">Купол</span>
      </RouterLink>
    </template>

    <nav aria-label="Основная навигация">
      <ul class="nav">
        <li><RouterLink to="/" @click="ui.closeMenu()">Главная</RouterLink></li>
        <li><RouterLink to="/catalog" @click="ui.closeMenu()">Каталог</RouterLink></li>
        <li v-if="auth.user"><RouterLink to="/file" @click="ui.closeMenu()">Личное дело</RouterLink></li>
        <li v-if="auth.can('team_panel')"><RouterLink to="/team" @click="ui.closeMenu()">Панель команды</RouterLink></li>
      </ul>
    </nav>

    <section class="account" aria-labelledby="menu-account-title">
      <h2 id="menu-account-title">Допуск</h2>
      <template v-if="auth.user">
        <p class="who">
          Вы вошли как <strong>{{ auth.user.login }}</strong><br>
          {{ auth.user.level_name }}
        </p>
      </template>
      <template v-else>
        <p class="who">Вы не вошли: уровень 0, Гражданин.</p>
        <UiButton variant="primary" block @click="ui.openAuth()">Войти или зарегистрироваться</UiButton>
      </template>
    </section>
  </UiDrawer>
</template>

<style scoped>
.brand {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text);
  text-decoration: none;
}
.brand__name {
  font-family: var(--font-head);
  font-size: var(--text-xl);
  font-weight: 700;
  letter-spacing: 0.2em;
  text-transform: uppercase;
}
.nav {
  margin: 0;
  padding: 0;
  border-top: 2px solid var(--ink-900);
  list-style: none;
}
.nav li {
  border-bottom: 1px solid var(--border);
}
.nav a {
  display: block;
  padding: var(--space-3) var(--space-3);
  color: var(--text);
  font-family: var(--font-head);
  font-size: var(--text-lg);
  letter-spacing: 0.12em;
  text-decoration: none;
  text-transform: uppercase;
}
.nav a:hover {
  background: var(--surface-strong);
}
.nav a[aria-current='page'] {
  background: var(--ink-900);
  color: var(--paper-50);
}
.account {
  margin-top: auto;
  padding-top: var(--space-4);
  border-top: 2px solid var(--ink-900);
}
.account h2 {
  margin-bottom: var(--space-2);
  font-size: var(--text-md);
}
.who {
  margin: 0 0 var(--space-3);
  overflow-wrap: anywhere;
}
</style>
