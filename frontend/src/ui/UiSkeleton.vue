<script setup lang="ts">
// Заглушка на время загрузки: строки разной длины вместо надписи «Загрузка…». Для скринридера — одно объявление.
withDefaults(defineProps<{ lines?: number; label?: string }>(), { lines: 3, label: undefined })
const widths = ['100%', '92%', '76%', '88%', '60%', '96%']
</script>

<template>
  <div class="ui-skeleton" role="status" :aria-label="label ?? $t('ui.loading')">
    <span class="visually-hidden">{{ label ?? $t('ui.loading') }}</span>
    <span v-for="i in lines" :key="i" class="ui-skeleton__line" aria-hidden="true" :style="{ width: widths[(i - 1) % widths.length] }" />
  </div>
</template>

<style scoped>
.ui-skeleton {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-2) 0;
}
.ui-skeleton__line {
  display: block;
  height: 0.9rem;
  border-radius: var(--radius-1);
  background: linear-gradient(100deg, var(--paper-200) 30%, var(--paper-100) 50%, var(--paper-200) 70%) 0 0 / 200% 100%;
  animation: ui-shimmer 1.4s linear infinite;
}
@keyframes ui-shimmer {
  to {
    background-position: -200% 0;
  }
}
</style>
