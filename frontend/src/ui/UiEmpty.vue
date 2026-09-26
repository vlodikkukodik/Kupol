<script setup lang="ts">
import UiIcon from './UiIcon.vue'
import type { IconName } from './icons'

// Пустое состояние: что произошло, почему и что делать дальше.
withDefaults(defineProps<{ icon?: IconName; title: string }>(), { icon: 'file' })
</script>

<template>
  <div class="ui-empty">
    <UiIcon :name="icon" size="2.5rem" />
    <p class="ui-empty__title">{{ title }}</p>
    <div class="ui-empty__more"><slot /></div>
  </div>
</template>

<style scoped>
.ui-empty {
  display: grid;
  justify-items: center;
  gap: var(--space-2);
  padding: var(--space-7) var(--space-4);
  border: 2px dashed var(--border-strong);
  border-radius: var(--radius-2);
  color: var(--text-muted);
  text-align: center;
}
.ui-empty__title {
  margin: 0;
  color: var(--text);
  font-family: var(--font-head);
  font-size: var(--text-lg);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.ui-empty__more {
  max-width: 34rem;
}
.ui-empty__more > :last-child {
  margin-bottom: 0;
}
</style>
