<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from './UiIcon.vue'

// Сообщение о результате: ошибка объявляется сразу (role=alert), остальное — вежливо (role=status).
const props = withDefaults(defineProps<{ tone?: 'info' | 'success' | 'warning' | 'danger'; title?: string; live?: boolean }>(), {
  tone: 'info',
  title: '',
  live: true,
})
const role = computed(() => (!props.live ? undefined : props.tone === 'danger' ? 'alert' : 'status'))
const icon = computed(() => (props.tone === 'danger' || props.tone === 'warning' ? 'alert' : props.tone === 'success' ? 'check' : 'info'))
</script>

<template>
  <div class="ui-alert" :class="`ui-alert--${tone}`" :role="role">
    <UiIcon :name="icon" size="1.35rem" />
    <div class="ui-alert__body">
      <strong v-if="title" class="ui-alert__title">{{ title }}</strong>
      <slot />
    </div>
  </div>
</template>

<style scoped>
.ui-alert {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  margin-bottom: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
  color: var(--text);
}
.ui-alert__body {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}
.ui-alert__body > :last-child {
  margin-bottom: 0;
}
.ui-alert__title {
  display: block;
  margin-bottom: var(--space-1);
  font-family: var(--font-head);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.ui-alert--danger {
  border-color: var(--red-700);
  background: #f8e6e1;
  color: var(--red-800);
}
.ui-alert--success {
  border-color: var(--green-700);
  background: #e6efe2;
  color: #1c4529;
}
.ui-alert--warning {
  border-color: var(--amber-700);
  background: #f6ecd0;
  color: #5c3806;
}
</style>
