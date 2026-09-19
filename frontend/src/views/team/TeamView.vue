<script setup>
import { useRoute } from 'vue-router'
import { useAuthStore } from '../../stores/auth.js'

const auth = useAuthStore()
const route = useRoute()
</script>

<template>
  <article class="team">
    <h1>Панель команды</h1>
    <nav class="sections" aria-label="Разделы панели команды">
      <RouterLink :to="{ name: 'team' }" exact-active-class="" :aria-current="route.name === 'team' ? 'page' : undefined">Роли и права</RouterLink>
      <RouterLink :to="{ name: 'team-documents' }" :aria-current="String(route.name).startsWith('team-document') ? 'page' : undefined">Документы</RouterLink>
      <RouterLink v-if="auth.can('manage_team')" :to="{ name: 'team-members' }">Команда</RouterLink>
    </nav>
    <RouterView />
  </article>
</template>

<style scoped>
.sections { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-bottom: var(--space-4); border-bottom: 2px solid var(--ink); }
.sections a {
  margin-bottom: -2px;
  padding: 0.5rem 1.2rem;
  border: 2px solid transparent;
  border-bottom: 0;
  color: var(--ink);
  font-family: var(--font-head);
  font-size: 1.05rem;
  letter-spacing: 0.1em;
  text-decoration: none;
  text-transform: uppercase;
}
.sections a:hover { background: rgb(0 0 0 / 0.06); }
.sections a[aria-current='page'] { border-color: var(--ink); background: var(--paper); font-weight: 700; }
</style>
