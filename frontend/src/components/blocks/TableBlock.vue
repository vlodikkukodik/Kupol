<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { caption, columns: [string], rows: [[runs]] }
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
          <td v-for="(cell, c) in row" :key="c"><RichText :runs="cell" /></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
/* Широкая таблица прокручивается внутри обёртки, страница не «ездит» вбок. */
.table-wrap { margin: var(--space-4) 0; overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 0.95rem; }
caption { margin-bottom: var(--space-2); text-align: left; font-family: var(--font-head); letter-spacing: 0.08em; text-transform: uppercase; }
th, td { padding: 0.35rem 0.6rem; border: 1px solid var(--ink); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
th { background: var(--paper-shade); font-family: var(--font-head); letter-spacing: 0.06em; }
</style>
