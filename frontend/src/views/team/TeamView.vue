<script setup lang="ts">
import { useRoute } from 'vue-router'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const route = useRoute()
</script>

<template>
  <div class="team">
    <UiPageHeader title="Панель команды" kicker="Рабочее место сотрудника архива" />
    <nav class="sections" aria-label="Разделы панели команды">
      <!-- Раздел помечается вручную: /team — префикс всех адресов панели, и роутер отметил бы «Роли и права» всегда -->
      <RouterLink :to="{ name: 'team' }" active-class="" exact-active-class="" :aria-current="route.name === 'team' ? 'page' : undefined">Рабочий стол</RouterLink>
      <RouterLink :to="{ name: 'team-documents' }" active-class="" exact-active-class="" :aria-current="String(route.name).startsWith('team-document') ? 'page' : undefined">Документы</RouterLink>
      <RouterLink :to="{ name: 'team-templates' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-templates' ? 'page' : undefined">Шаблоны</RouterLink>
      <RouterLink :to="{ name: 'team-glossary' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-glossary' ? 'page' : undefined">Глоссарий</RouterLink>
      <RouterLink :to="{ name: 'team-timeline' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-timeline' ? 'page' : undefined">Хронология</RouterLink>
      <RouterLink :to="{ name: 'team-site' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-site' ? 'page' : undefined">Сайт</RouterLink>
      <RouterLink :to="{ name: 'team-roles' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-roles' ? 'page' : undefined">Роли и права</RouterLink>
      <RouterLink v-if="auth.can('manage_team')" :to="{ name: 'team-members' }" active-class="" exact-active-class="" :aria-current="route.name === 'team-members' ? 'page' : undefined">Команда</RouterLink>
    </nav>
    <RouterView />
  </div>
</template>

<style scoped>
.sections {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  margin-bottom: var(--space-5);
}
.sections a {
  min-height: var(--control-h);
  display: inline-flex;
  align-items: center;
  padding: 0 var(--space-5);
  border: 1.5px solid var(--line-on-bg);
  border-radius: var(--radius-2);
  color: var(--on-bg);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-decoration: none;
  text-transform: uppercase;
}
.sections a:hover {
  background: rgb(255 255 255 / 0.08);
}
.sections a[aria-current='page'] {
  border-color: var(--paper-100);
  background: var(--paper-100);
  color: var(--ink-900);
  font-weight: 700;
}
</style>
