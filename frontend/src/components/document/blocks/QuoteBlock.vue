<script setup lang="ts">
import type { OutQuote } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutQuote }>()
</script>

<template>
  <figure class="quote">
    <blockquote><RichRuns :runs="data.text" /></blockquote>
    <figcaption v-if="data.source">— {{ data.source }}</figcaption>
  </figure>
</template>

<style scoped>
.quote {
  margin: var(--space-5) 0;
  padding-left: var(--space-4);
  border-left: 4px solid var(--paper-400);
}
blockquote {
  margin: 0;
  font-style: italic;
  white-space: pre-line;
  overflow-wrap: anywhere;
}
figcaption {
  margin-top: var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
