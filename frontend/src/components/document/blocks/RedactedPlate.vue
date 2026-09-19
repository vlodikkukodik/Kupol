<script setup lang="ts">
import type { RedactedData } from '@/api/blocks'
import { requiredAccess } from '@/lib/levels'

// Закрытый блок: сервер прислал только нужный уровень допуска — ни текста, ни длины, ни вида блока.
defineProps<{ data: RedactedData }>()
</script>

<template>
  <div class="plate" role="img" :aria-label="`Засекреченный фрагмент: ${requiredAccess(data.level)}`" :data-level="data.level">
    <span class="plate__title">Данные удалены</span>
    <span class="plate__need">{{ requiredAccess(data.level) }}</span>
  </div>
</template>

<style scoped>
.plate {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  margin: var(--space-4) 0;
  padding: var(--space-3) var(--space-4);
  background: var(--redaction);
  color: var(--paper-100);
  font-family: var(--font-head);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  user-select: none;
  /* сплошная чёрная полоса: надпись на ней должна читаться, а рваный край даёт тень */
  box-shadow: 0 0 0 1px var(--redaction), 1px 1px 0 1px rgb(0 0 0 / 0.35);
}
.plate__title {
  font-size: var(--text-lg);
  font-weight: 700;
}
.plate__need {
  font-size: var(--text-sm);
  opacity: 0.9;
}
</style>
