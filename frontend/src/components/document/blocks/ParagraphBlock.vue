<script setup lang="ts">
import type { OutParagraph } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutParagraph }>()
</script>

<template>
  <p class="paragraph"><RichRuns :runs="data.text" /></p>
</template>

<style scoped>
.paragraph {
  margin: 0 0 var(--space-4);
  white-space: pre-line;
  overflow-wrap: anywhere;
}
</style>
