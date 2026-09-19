<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  id: { type: String, required: true },
  label: { type: String, required: true },
  modelValue: { type: String, default: '' },
  type: { type: String, default: 'text' },
  autocomplete: { type: String, default: 'off' },
  error: { type: String, default: '' },
  hint: { type: String, default: '' },
  maxlength: { type: Number, default: undefined },
  inputmode: { type: String, default: undefined },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const input = ref(null)
const revealed = ref(false)
const isPassword = computed(() => props.type === 'password')
const inputType = computed(() => (isPassword.value && revealed.value ? 'text' : props.type))
const describedBy = computed(
  () =>
    [props.hint ? `${props.id}-hint` : '', props.error ? `${props.id}-error` : ''].filter(Boolean).join(' ') || undefined,
)

defineExpose({ focus: () => input.value?.focus() })
</script>

<template>
  <div class="field" :class="{ 'field--invalid': error }">
    <label :for="id">{{ label }}</label>
    <div class="control">
      <input
        :id="id"
        ref="input"
        :type="inputType"
        :value="modelValue"
        :autocomplete="autocomplete"
        :maxlength="maxlength"
        :inputmode="inputmode"
        :disabled="disabled"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy"
        spellcheck="false"
        autocapitalize="off"
        autocorrect="off"
        @input="emit('update:modelValue', $event.target.value)"
      >
      <button
        v-if="isPassword"
        type="button"
        class="reveal"
        :aria-pressed="revealed ? 'true' : 'false'"
        :aria-controls="id"
        @click="revealed = !revealed"
      >
        {{ revealed ? 'Скрыть' : 'Показать' }}
      </button>
    </div>
    <p v-if="hint" :id="`${id}-hint`" class="hint">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="error">{{ error }}</p>
  </div>
</template>

<style scoped>
.field { margin-bottom: var(--space-3); }
label {
  display: block;
  margin-bottom: var(--space-1);
  font-family: var(--font-head);
  font-size: 0.95rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.control { display: flex; gap: var(--space-2); }
input {
  flex: 1;
  min-width: 0;
  padding: 0.55rem 0.7rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: #f4eedc;
  color: var(--ink);
  font: inherit;
}
.field--invalid input { border-color: var(--stamp-red); }
.reveal {
  flex: none;
  padding: 0 0.8rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink);
  font-size: 0.85rem;
  cursor: pointer;
}
.reveal:hover { background: var(--ink); color: var(--paper); }
.hint { margin: var(--space-1) 0 0; font-size: 0.85rem; color: var(--ink-soft); }
.error { margin: var(--space-1) 0 0; font-size: 0.9rem; color: var(--stamp-red); font-weight: 700; }
</style>
