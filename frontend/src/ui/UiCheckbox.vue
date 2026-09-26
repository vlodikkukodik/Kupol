<script setup lang="ts">
import { ref, useId } from 'vue'

const model = defineModel<boolean>({ default: false })
const props = defineProps<{ label: string; disabled?: boolean; id?: string; invalid?: boolean; describedby?: string }>()
defineEmits<{ change: [event: Event] }>()
const uid = useId()
const id = props.id ?? `cb-${uid}`
const el = ref<HTMLInputElement | null>(null)
defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <div class="ui-checkbox">
    <input
      :id="id"
      ref="el"
      v-model="model"
      type="checkbox"
      :disabled="disabled"
      :aria-invalid="invalid ? 'true' : undefined"
      :aria-describedby="describedby"
      @change="$emit('change', $event)"
    >
    <label :for="id">{{ label }}</label>
  </div>
</template>

<style scoped>
.ui-checkbox {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: var(--control-h);
}
input {
  flex: none;
  appearance: none;
  width: 1.5rem;
  height: 1.5rem;
  margin: 0;
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-1);
  background: var(--surface-raised);
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease);
}
input:checked {
  border-color: var(--ink-900);
  background: var(--ink-900)
    url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23f8f4e8' stroke-width='3' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M5 12l5 5 9-10'/%3E%3C/svg%3E")
    center / 80% no-repeat;
}
input:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
label {
  cursor: pointer;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
</style>
