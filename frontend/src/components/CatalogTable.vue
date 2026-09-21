<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import type { Item } from '@/api/generated/documents'
import { computed } from 'vue'
import { ariaSort, nextSort, statusNameLower, type SortKey } from '@/lib/catalog'
import { t } from '@/i18n'

defineProps<{
  items: Item[]
  /** Директорат видит статус документа */
  showStatus?: boolean
}>()

const route = useRoute()
const router = useRouter()

// Неопубликованное видит только Директорат; статус в реестре — словами архива (statusNameLower).
const COLUMNS = computed<{ key: SortKey; label: string }[]>(() => [
  { key: 'code', label: t('catalog.col.code') },
  { key: 'title', label: t('catalog.col.title') },
  { key: 'class', label: t('catalog.col.class') },
  { key: 'year', label: t('catalog.col.year') },
  { key: 'deviation', label: t('catalog.col.deviation') },
])
const ARROWS = { ascending: '↑', descending: '↓', none: '' }

function sortBy(key: SortKey) {
  void router.push({ query: nextSort(route.query, key) })
}
</script>

<template>
  <div class="wrap" tabindex="0" role="region" :aria-label="$t('catalog.registry')">
    <table class="registry" data-testid="registry">
      <caption class="visually-hidden">{{ $t('catalog.registry') }}</caption>
      <thead>
        <tr>
          <th v-for="c in COLUMNS" :key="c.key" scope="col" :aria-sort="ariaSort(route.query, c.key)">
            <button type="button" class="sort" @click="sortBy(c.key)">
              {{ c.label }}
              <span class="arrow" aria-hidden="true">{{ ARROWS[ariaSort(route.query, c.key)] }}</span>
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
              {{ it.type_name }}<template v-if="it.department"> · {{ it.department }}</template><template v-if="it.level > 0"> · {{ $t('catalog.access', { level: it.level }) }}</template><template v-if="showStatus && it.status && it.status !== 'published'"> · {{ statusNameLower(it.status) }}</template>
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
.wrap {
  overflow-x: auto;
}
.registry {
  width: 100%;
  border-collapse: collapse;
}
.registry th {
  padding: 0;
  border-bottom: 2px solid var(--ink-900);
  text-align: left;
}
.sort {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  width: 100%;
  min-height: var(--control-h);
  padding: 0 var(--space-3);
  border: 0;
  background: transparent;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.sort:hover {
  background: var(--surface-strong);
}
th[aria-sort='ascending'] .sort,
th[aria-sort='descending'] .sort {
  background: var(--surface-strong);
  font-weight: 700;
}
.arrow {
  min-width: 1ch;
}
.registry td {
  padding: var(--space-3);
  border-bottom: 1px dashed var(--border-strong);
  vertical-align: top;
}
.registry tbody tr:hover {
  background: rgb(255 255 255 / 0.4);
}
.code {
  white-space: nowrap;
  font-family: var(--font-doc);
  font-size: var(--text-sm);
  font-weight: 700;
}
.title a {
  font-size: var(--text-md);
  font-weight: 700;
}
.sub {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.num {
  font-family: var(--font-doc);
  text-align: right;
  white-space: nowrap;
}
.registry th:nth-child(n + 3) .sort {
  justify-content: flex-end;
}
</style>
