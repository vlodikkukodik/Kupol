<script setup>
import { requiredAccess } from '../../lib/levels.js'

defineProps({
  data: { type: Object, required: true }, // { level }
})
</script>

<template>
  <div class="plate" role="img" :aria-label="`Засекреченный фрагмент: ${requiredAccess(data.level)}`" :data-level="data.level">
    <span class="title">Данные удалены</span>
    <span class="need">{{ requiredAccess(data.level) }}</span>
  </div>
</template>

<style scoped>
.plate {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  margin: var(--space-3) 0;
  padding: var(--space-3) var(--space-4);
  background: var(--ink);
  color: var(--paper);
  font-family: var(--font-head);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  user-select: none;
}
.title { font-size: 1.15rem; font-weight: 700; }
.need { font-size: 0.95rem; opacity: 0.9; }
</style>
