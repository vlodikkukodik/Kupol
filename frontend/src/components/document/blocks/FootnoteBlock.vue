<script setup lang="ts">
import type { OutFootnote } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutFootnote }>()
</script>

<template>
  <p class="footnote"><sup>{{ data.mark }}</sup> <RichRuns :runs="data.text" /></p>
</template>

<style scoped>
.footnote {
  margin: var(--space-2) 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
</style>
