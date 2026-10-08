<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { inboxApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import UiButton from '@/ui/UiButton.vue'
import UiDrawer from '@/ui/UiDrawer.vue'
import UiSeal from '@/ui/UiSeal.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

// Боковое меню: разделы архива и сведения о допуске читателя.
const ui = useUiStore()
const auth = useAuthStore()
const toggle = () => document.getElementById('menu-toggle')
// Значок непрочитанных записок (шаг 5.7): опрос раз в минуту, пока пользователь вошёл
const unread = useQuery({
  queryKey: keys.inboxUnread,
  queryFn: ({ signal }) => inboxApi.unread({ signal }),
  enabled: computed(() => auth.user !== null),
  refetchInterval: 60_000,
})
</script>

<template>
  <UiDrawer
    id="site-menu"
    :open="ui.menuOpen"
    :label="$t('app.menu')"
    :close-label="$t('app.closeMenu')"
    scrim-testid="menu-scrim"
    :fallback-focus="toggle"
    @update:open="(v: boolean) => !v && ui.closeMenu()"
  >
    <template #head>
      <RouterLink to="/" class="brand" @click="ui.closeMenu()">
        <UiSeal :size="38" decorative />
        <span class="brand__name">{{ $t('app.brandName') }}</span>
      </RouterLink>
    </template>

    <nav :aria-label="$t('app.mainNav')">
      <ul class="nav">
        <li><RouterLink to="/" @click="ui.closeMenu()">{{ $t('app.nav.home') }}</RouterLink></li>
        <li><RouterLink to="/catalog" @click="ui.closeMenu()">{{ $t('app.nav.catalog') }}</RouterLink></li>
        <li><RouterLink to="/search" @click="ui.closeMenu()">{{ $t('app.nav.search') }}</RouterLink></li>
        <li><RouterLink to="/about" @click="ui.closeMenu()">{{ $t('app.nav.about') }}</RouterLink></li>
        <li v-if="auth.user"><RouterLink to="/file" @click="ui.closeMenu()">{{ $t('app.nav.file') }}</RouterLink></li>
        <li v-if="auth.user">
          <RouterLink to="/inbox" @click="ui.closeMenu()">{{ $t('app.nav.inbox') }}<span v-if="unread.data.value" class="badge" data-testid="inbox-badge">{{ unread.data.value }}</span></RouterLink>
        </li>
        <li v-if="auth.user"><RouterLink to="/suggestions" @click="ui.closeMenu()">{{ $t('app.nav.suggestions') }}</RouterLink></li>
        <li v-if="auth.can('team_panel')"><RouterLink to="/team" @click="ui.closeMenu()">{{ $t('app.nav.team') }}</RouterLink></li>
      </ul>
    </nav>

    <section class="account" aria-labelledby="menu-account-title">
      <h2 id="menu-account-title">{{ $t('app.access.title') }}</h2>
      <template v-if="auth.user">
        <p class="who">
          <i18n-t keypath="app.access.signedInAs" scope="global"><template #login><strong>{{ auth.user.login }}</strong></template></i18n-t><br>
          {{ auth.user.level_name }}
        </p>
      </template>
      <template v-else>
        <p class="who">{{ $t('app.access.guest') }}</p>
        <UiButton variant="primary" block @click="ui.openAuth()">{{ $t('app.access.signIn') }}</UiButton>
      </template>
    </section>
  </UiDrawer>
</template>

<style scoped>
.badge {
  display: inline-block;
  min-width: 1.4em;
  margin-left: var(--space-2);
  padding: 0 0.35em;
  border-radius: 999px;
  background: var(--red-700);
  color: var(--paper-50);
  font-size: var(--text-xs);
  line-height: 1.5;
  text-align: center;
}
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
