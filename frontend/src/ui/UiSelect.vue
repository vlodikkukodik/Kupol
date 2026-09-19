<script setup lang="ts">
import { useField } from './field'
import './controls.css'

// Обычный <select>: на телефоне открывается системный выбор — он удобнее любого самодельного.
export interface SelectOption {
  value: string
  label: string
}
const model = defineModel<string>({ default: '' })
defineProps<{ options: SelectOption[]; placeholder?: string; disabled?: boolean; name?: string }>()
defineEmits<{ change: [event: Event] }>()
const field = useField()
</script>

<template>
  <select
    :id="field?.id"
    v-model="model"
    class="ui-control"
    :name="name"
    :disabled="disabled"
    :required="field?.required"
    :aria-invalid="field?.invalid ? 'true' : undefined"
    :aria-describedby="field?.describedBy"
    @change="$emit('change', $event)"
  >
    <option v-if="placeholder !== undefined" value="">{{ placeholder }}</option>
    <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
  </select>
</template>
