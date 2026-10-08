<script setup lang="ts">
import type { OutQA } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutQA }>()
</script>

<template>
  <dl class="qa">
    <template v-for="(e, i) in data.entries" :key="i">
      <dt>{{ $t('doc.block.qaQuestion') }}</dt>
      <dd><RichRuns :runs="e.question" /></dd>
      <dt>{{ $t('doc.block.qaAnswer') }}</dt>
      <dd><RichRuns :runs="e.answer" /></dd>
    </template>
  </dl>
</template>

<style scoped>
.qa {
  margin: var(--space-5) 0;
}
.qa dt {
  margin-top: var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
  text-transform: uppercase;
  letter-spacing: var(--tracking-caps);
}
.qa dt:first-child {
  margin-top: 0;
}
.qa dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
