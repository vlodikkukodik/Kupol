<script setup lang="ts">
import type { OutDivider } from '@/api/generated/documents'

defineProps<{ data: OutDivider }>()
</script>

<template>
  <p v-if="data.style === 'stars'" class="stars" aria-hidden="true">* * *</p>
  <hr v-else class="line">
</template>

<style scoped>
.line {
  margin: var(--space-5) 0;
  border: 0;
  border-top: 2px solid var(--ink-900);
}
.stars {
  margin: var(--space-5) 0;
  color: var(--text-muted);
  letter-spacing: 0.6em;
  text-align: center;
}
</style>
