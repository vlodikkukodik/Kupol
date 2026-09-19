<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { pageWindow } from '../lib/catalog.js'

const props = defineProps({
  page: { type: Number, required: true },
  pages: { type: Number, required: true },
})

const route = useRoute()
const items = computed(() => pageWindow(props.page, props.pages))

// Ссылки — настоящие адреса: «открыть в новой вкладке» и «назад» работают.
function to(p) {
  const query = { ...route.query }
  if (p <= 1) delete query.page
  else query.page = String(p)
  return { query }
}
</script>

<template>
  <nav v-if="pages > 1" class="pages" aria-label="Страницы каталога">
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
.pages { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; margin-top: var(--space-4); }
.num, .current, .step {
  min-width: 2.2rem;
  padding: 0.3rem 0.7rem;
  border: 2px solid var(--ink);
  text-align: center;
  text-decoration: none;
  color: var(--ink);
}
.current { background: var(--ink); color: var(--paper); font-weight: 700; }
a:hover { background: var(--paper-shade); }
.gap { padding: 0 0.2rem; color: var(--ink-soft); }
</style>
