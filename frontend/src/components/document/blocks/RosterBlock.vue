<script setup lang="ts">
import type { OutRoster } from '@/api/generated/documents'

defineProps<{ data: OutRoster }>()
</script>

<template>
  <table class="roster" :aria-label="$t('doc.block.roster')">
    <tbody>
      <tr v-for="(e, i) in data.entries" :key="i">
        <td class="roster__name">{{ e.name }}</td>
        <td v-if="e.position" class="roster__position">{{ e.position }}</td>
      </tr>
    </tbody>
  </table>
</template>

<style scoped>
.roster {
  width: 100%;
  margin: var(--space-5) 0;
  border-collapse: collapse;
}
.roster td {
  padding: var(--space-1) var(--space-3);
  border-top: 1px solid var(--border-strong);
  overflow-wrap: anywhere;
}
.roster tr:first-child td {
  border-top: 0;
}
.roster__name {
  font-weight: 700;
}
.roster__position {
  color: var(--text-muted);
}
</style>
