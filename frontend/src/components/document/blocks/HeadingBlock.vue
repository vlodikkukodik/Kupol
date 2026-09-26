<script setup lang="ts">
import { computed } from 'vue'
import type { OutHeading } from '@/api/generated/documents'

const props = defineProps<{ data: OutHeading }>()

// h1 занят названием документа: глубина 1 → h2, 2 → h3, 3 → h4
const tag = computed(() => `h${Math.min(4, Math.max(2, props.data.depth + 1))}`)
</script>

<template>
  <component :is="tag" class="heading">{{ data.text }}</component>
</template>

<style scoped>
.heading {
  margin: var(--space-6) 0 var(--space-3);
  font-family: var(--font-head);
}
h2.heading {
  padding-bottom: var(--space-1);
  border-bottom: 2px solid var(--ink-900);
}
h3.heading {
  font-size: var(--text-lg);
}
h4.heading {
  font-size: var(--text-md);
  color: var(--text-muted);
}
</style>
