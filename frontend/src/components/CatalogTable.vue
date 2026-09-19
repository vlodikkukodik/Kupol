<script setup>
import { useRoute, useRouter } from 'vue-router'
import { ariaSort, nextSort } from '../lib/catalog.js'

defineProps({
  items: { type: Array, required: true },
  showStatus: { type: Boolean, default: false }, // Директорат видит статус документа
})

const route = useRoute()
const router = useRouter()

// Неопубликованное видит только Директорат; статус в реестре — словами архива.
const STATUS_NAMES = { draft: 'черновик', review: 'на проверке', archived: 'в архиве' }

const COLUMNS = [
  { key: 'code', label: '№' },
  { key: 'title', label: 'Название' },
  { key: 'class', label: 'Класс' },
  { key: 'year', label: 'Год' },
  { key: 'deviation', label: 'П.о.' },
]

function sortBy(key) {
  router.push({ query: nextSort(route.query, key) })
}
</script>

<template>
  <div class="wrap" tabindex="0" role="region" aria-label="Реестр документов">
    <table class="registry" data-testid="registry">
      <caption class="visually-hidden">Реестр документов</caption>
      <thead>
        <tr>
          <th v-for="c in COLUMNS" :key="c.key" scope="col" :aria-sort="ariaSort(route.query, c.key)">
            <button type="button" class="sort" @click="sortBy(c.key)">
              {{ c.label }}
              <span class="arrow" aria-hidden="true">{{ { ascending: '↑', descending: '↓', none: '' }[ariaSort(route.query, c.key)] }}</span>
            </button>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="it in items" :key="it.slug">
          <td class="code"><RouterLink :to="{ name: 'document', params: { ref: it.slug } }">{{ it.code }}</RouterLink></td>
          <td class="title">
            <RouterLink :to="{ name: 'document', params: { ref: it.slug } }">{{ it.title }}</RouterLink>
            <span class="sub">
              {{ it.type_name }}<template v-if="it.department"> · {{ it.department }}</template><template v-if="it.level > 0"> · допуск {{ it.level }}</template><template v-if="showStatus && it.status && it.status !== 'published'"> · {{ STATUS_NAMES[it.status] ?? it.status }}</template>
            </span>
          </td>
          <td class="num">{{ it.danger_class ?? '—' }}</td>
          <td class="num">{{ it.composed.year }}</td>
          <td class="num">{{ it.deviation_points ?? '—' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.wrap { overflow-x: auto; }
.registry { width: 100%; border-collapse: collapse; }
th, td { padding: 0.5rem 0.7rem; border-bottom: 1px solid var(--rule); text-align: left; vertical-align: top; }
thead th { border-bottom: 2px solid var(--ink); background: var(--paper-shade); white-space: nowrap; }
.sort {
  padding: 0;
  border: 0;
  background: none;
  color: var(--ink);
  font-family: var(--font-head);
  font-size: 0.95rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  cursor: pointer;
}
.sort:hover { text-decoration: underline; }
.arrow { display: inline-block; min-width: 1ch; }
.code { white-space: nowrap; font-weight: 700; }
.title { min-width: 12rem; overflow-wrap: anywhere; }
.sub { display: block; font-size: 0.8rem; color: var(--ink-soft); }
.num { text-align: right; white-space: nowrap; }
td a { color: var(--ink); }
td a:hover { color: var(--stamp-red); }
</style>
