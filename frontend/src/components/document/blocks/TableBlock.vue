<script setup lang="ts">
import type { OutTable } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutTable }>()
</script>

<template>
  <div class="table-wrap" tabindex="0" role="region" :aria-label="data.caption || 'Таблица'">
    <table>
      <caption v-if="data.caption">{{ data.caption }}</caption>
      <thead>
        <tr><th v-for="(c, i) in data.columns" :key="i" scope="col">{{ c }}</th></tr>
      </thead>
      <tbody>
        <tr v-for="(row, r) in data.rows" :key="r">
          <td v-for="(cell, c) in row" :key="c"><RichRuns :runs="cell" /></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
/* Широкая таблица прокручивается внутри обёртки, страница не «ездит» вбок. */
.table-wrap {
  margin: var(--space-5) 0;
  overflow-x: auto;
}
table {
  width: 100%;
  font-size: var(--text-sm);
}
caption {
  margin-bottom: var(--space-2);
  text-align: left;
  font-family: var(--font-head);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
th,
td {
  padding: 0.35rem 0.6rem;
  border: 1px solid var(--ink-900);
  text-align: left;
  vertical-align: top;
  overflow-wrap: anywhere;
}
th {
  background: var(--surface-sunken);
  font-family: var(--font-head);
  letter-spacing: 0.06em;
}
</style>
