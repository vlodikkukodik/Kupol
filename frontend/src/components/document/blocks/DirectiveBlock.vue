<script setup lang="ts">
import type { OutDirective } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutDirective }>()
</script>

<template>
  <section class="directive">
    <p class="directive__recipient">{{ data.recipient }}</p>
    <p class="directive__order"><RichRuns :runs="data.order" /></p>
    <p v-if="data.deadline" class="directive__deadline">{{ $t('doc.block.directiveDeadline', { deadline: data.deadline }) }}</p>
  </section>
</template>

<style scoped>
.directive {
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border-left: 3px solid var(--ink-900);
}
.directive__recipient {
  margin: 0 0 var(--space-1);
  font-weight: 700;
  text-transform: uppercase;
}
.directive__order {
  margin: 0;
  overflow-wrap: anywhere;
}
.directive__deadline {
  margin: var(--space-2) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
