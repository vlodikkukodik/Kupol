<script setup lang="ts">
import { computed, ref } from 'vue'
import { useField } from './field'
import './controls.css'

const model = defineModel<string>({ default: '' })
const props = withDefaults(
  defineProps<{
    type?: 'text' | 'password' | 'search' | 'number' | 'email' | 'url'
    autocomplete?: string
    placeholder?: string
    maxlength?: number
    inputmode?: 'text' | 'numeric' | 'decimal' | 'search' | 'email' | 'url'
    disabled?: boolean
    readonly?: boolean
    min?: number | string
    max?: number | string
    name?: string
    /** Пароль: кнопка «Показать / Скрыть» справа */
    reveal?: boolean
  }>(),
  { type: 'text', autocomplete: 'off', reveal: false },
)
defineEmits<{ change: [event: Event] }>()

const field = useField()
const el = ref<HTMLInputElement | null>(null)
const revealed = ref(false)
const kind = computed(() => (props.type === 'password' && revealed.value ? 'text' : props.type))
defineExpose({ focus: () => el.value?.focus(), el })
</script>

<template>
  <div class="ui-input">
    <input
      :id="field?.id"
      ref="el"
      :value="model"
      class="ui-control"
      :type="kind"
      :name="name"
      :autocomplete="autocomplete"
      :placeholder="placeholder"
      :maxlength="maxlength"
      :inputmode="inputmode"
      :disabled="disabled"
      :readonly="readonly"
      :min="min"
      :max="max"
      :required="field?.required"
      :aria-invalid="field?.invalid ? 'true' : undefined"
      :aria-describedby="field?.describedBy"
      spellcheck="false"
      autocapitalize="off"
      autocorrect="off"
      @input="model = ($event.target as HTMLInputElement).value"
      @change="$emit('change', $event)"
    >
    <button
      v-if="reveal && type === 'password'"
      type="button"
      class="ui-input__reveal"
      :aria-pressed="revealed ? 'true' : 'false'"
      :aria-controls="field?.id"
      @click="revealed = !revealed"
    >
      {{ revealed ? $t('ui.hide') : $t('ui.show') }}
    </button>
  </div>
</template>

<style scoped>
.ui-input {
  display: flex;
  gap: var(--space-2);
}
.ui-input .ui-control {
  flex: 1;
  min-width: 0;
}
.ui-input__reveal {
  flex: none;
  min-height: var(--control-h);
  padding: 0 var(--space-3);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
  font-size: var(--text-sm);
  cursor: pointer;
}
.ui-input__reveal:hover {
  background: var(--surface-strong);
}
.ui-input__reveal[aria-pressed='true'] {
  background: var(--ink-900);
  color: var(--paper-50);
  border-color: var(--ink-900);
}
</style>
