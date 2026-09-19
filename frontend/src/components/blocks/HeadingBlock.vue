<script setup>
import { computed } from 'vue'

const props = defineProps({ data: { type: Object, required: true } }) // { depth, text }

// h1 занят названием документа: глубина 1 -> h2, 2 -> h3, 3 -> h4
const tag = computed(() => `h${Math.min(4, Math.max(2, props.data.depth + 1))}`)
</script>

<template>
  <component :is="tag" class="heading">{{ data.text }}</component>
</template>

<style scoped>
.heading { margin: var(--space-5) 0 var(--space-3); }
h3.heading { font-size: 1.2rem; }
h4.heading { font-size: 1.05rem; color: var(--ink-soft); }
</style>
