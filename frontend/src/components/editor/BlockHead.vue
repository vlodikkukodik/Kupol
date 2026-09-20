<script setup lang="ts">
import { computed } from 'vue'
import { NODE_LABELS } from '@/editor/fields'
import { levelName } from '@/lib/levels'
import UiIcon from '@/ui/UiIcon.vue'

// Шапка блока в редакторе: вид блока и, если блок закрыт, — до какого уровня. Правится допуск панелью инструментов.
const props = defineProps<{ name: string; level: number | null }>()
const label = computed(() => NODE_LABELS[props.name] ?? props.name)
</script>

<template>
  <div class="pm-head" contenteditable="false">
    <span class="pm-head__label">{{ label }}</span>
    <span v-if="level !== null && level > 0" class="pm-head__level" data-testid="block-level">
      <UiIcon name="lock" size="1em" />
      Закрыт до уровня {{ level }} · {{ levelName(level) }}
    </span>
  </div>
</template>

<style scoped>
.pm-head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
  margin-bottom: var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  user-select: none;
}
.pm-head__label {
  color: var(--text-muted);
  font-weight: 500;
}
.pm-head__level {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-2);
  border-radius: var(--radius-1);
  background: var(--redaction);
  color: var(--paper-50);
  font-weight: 700;
}
</style>
