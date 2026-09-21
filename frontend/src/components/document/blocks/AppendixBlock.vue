<script setup lang="ts">
import type { OutAppendix } from '@/api/generated/documents'

defineProps<{ data: OutAppendix }>()
</script>

<template>
  <h2 class="appendix">
    <span class="appendix__mark">{{ data.number ? $t('doc.block.appendixNumber', { n: data.number }) : $t('doc.block.appendix') }}</span>
    <span>{{ data.title }}</span>
  </h2>
</template>

<style scoped>
.appendix {
  margin: var(--space-7) 0 var(--space-3);
  padding: var(--space-2) 0;
  border-top: 3px double var(--ink-900);
  border-bottom: 1px solid var(--ink-900);
  font-size: var(--text-lg);
}
.appendix__mark {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
