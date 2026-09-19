<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, type RouteLocationRaw } from 'vue-router'
import { pageWindow } from '@/lib/catalog'

const props = defineProps<{ page: number; pages: number; label?: string }>()

const route = useRoute()
const items = computed(() => pageWindow(props.page, props.pages))

// Ссылки — настоящие адреса: «открыть в новой вкладке» и «назад» работают.
function to(p: number): RouteLocationRaw {
  const query = { ...route.query }
  if (p <= 1) delete query.page
  else query.page = String(p)
  return { query }
}
</script>

<template>
  <nav v-if="pages > 1" class="pages" :aria-label="label ?? 'Страницы каталога'">
    <!-- Роутер сравнивает только путь, а не query, и сам поставил бы aria-current="page" на все ссылки:
         текущая страница помечена вручную, у ссылок метка выключена. -->
    <RouterLink v-if="page > 1" class="step" :to="to(page - 1)" rel="prev" aria-current-value="false">← Назад</RouterLink>
    <template v-for="(p, i) in items" :key="i">
      <span v-if="p === null" class="gap" aria-hidden="true">…</span>
      <span v-else-if="p === page" class="current" aria-current="page">{{ p }}</span>
      <RouterLink v-else class="num" :to="to(p)" :aria-label="`Страница ${p}`" aria-current-value="false">{{ p }}</RouterLink>
    </template>
    <RouterLink v-if="page < pages" class="step" :to="to(page + 1)" rel="next" aria-current-value="false">Далее →</RouterLink>
  </nav>
</template>

<style scoped>
.pages {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-items: center;
  margin-top: var(--space-5);
}
.num,
.current,
.step {
  display: inline-grid;
  place-items: center;
  min-width: var(--control-h);
  min-height: var(--control-h);
  padding: 0 var(--space-3);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  color: var(--text);
  text-decoration: none;
}
.current {
  background: var(--ink-900);
  color: var(--paper-50);
  font-weight: 700;
}
a:hover {
  background: var(--surface-strong);
}
.gap {
  padding: 0 var(--space-1);
  color: var(--text-muted);
}
</style>
