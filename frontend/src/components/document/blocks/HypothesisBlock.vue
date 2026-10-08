<script setup lang="ts">
import type { OutHypothesis } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutHypothesis }>()
</script>

<template>
  <section class="hypothesis">
    <p><strong>{{ $t('doc.block.hypothesis') }}:</strong> <RichRuns :runs="data.text" /></p>
    <p><strong>{{ $t('doc.block.hypothesisResult') }}:</strong> <RichRuns :runs="data.result" /></p>
    <p v-if="data.confirmed !== undefined" class="hypothesis__verdict">
      {{ data.confirmed ? $t('doc.block.hypothesisConfirmed') : $t('doc.block.hypothesisRefuted') }}
    </p>
  </section>
</template>

<style scoped>
.hypothesis {
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border: 1px dashed var(--border-strong);
}
.hypothesis p {
  margin: 0 0 var(--space-2);
  overflow-wrap: anywhere;
}
.hypothesis p:last-child {
  margin-bottom: 0;
}
.hypothesis__verdict {
  font-weight: 700;
  text-transform: uppercase;
  font-size: var(--text-sm);
}
</style>
