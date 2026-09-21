<script setup lang="ts">
import type { OutPage } from '@/api/generated/documents'

defineProps<{ data: OutPage }>()
</script>

<template>
  <p class="page">{{ data.number ? $t('doc.block.page', { n: data.number }) : $t('doc.block.pageNoNumber') }}</p>
</template>

<style scoped>
.page {
  margin: var(--space-5) 0;
  padding-top: var(--space-2);
  border-top: 1px dashed var(--border-strong);
  color: var(--text-muted);
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  text-align: center;
}
</style>
