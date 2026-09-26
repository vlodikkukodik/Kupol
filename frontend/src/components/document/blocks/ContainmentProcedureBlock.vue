<script setup lang="ts">
import type { OutContainmentProcedure } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutContainmentProcedure }>()
</script>

<template>
  <section class="procedure" :aria-label="data.title || $t('doc.block.containment')">
    <h3 class="procedure__title">{{ data.title || $t('doc.block.containment') }}</h3>
    <ol class="procedure__steps">
      <li v-for="(step, i) in data.steps" :key="i"><RichRuns :runs="step" /></li>
    </ol>
  </section>
</template>

<style scoped>
.procedure {
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--ink-900);
  background: var(--surface-sunken);
}
.procedure__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-lg);
}
.procedure__steps {
  margin: 0;
  padding-left: 1.4em;
}
.procedure__steps li {
  padding: var(--space-1) 0;
  overflow-wrap: anywhere;
}
</style>
