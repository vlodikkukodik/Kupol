<script setup lang="ts">
import { computed, reactive, useId } from 'vue'
import { provideField } from './field'

// Подпись, подсказка и ошибка вокруг одного поля. Само поле (UiInput, UiSelect, UiTextarea…) берёт идентификатор
// и aria-связи отсюда: подпись всегда привязана к полю, ошибка озвучивается вместе с ним.
const props = withDefaults(
  defineProps<{ label: string; hint?: string; error?: string; required?: boolean; id?: string; hideLabel?: boolean }>(),
  { hint: '', error: '', required: false, id: '', hideLabel: false },
)

const uid = useId()
const id = computed(() => props.id || `f-${uid}`)
const hintId = computed(() => `${id.value}-hint`)
const errorId = computed(() => `${id.value}-error`)

const ctx = reactive({
  id,
  describedBy: computed(() => [props.hint ? hintId.value : '', props.error ? errorId.value : ''].filter(Boolean).join(' ') || undefined),
  invalid: computed(() => Boolean(props.error)),
  required: computed(() => props.required),
})
provideField(ctx)
</script>

<template>
  <div class="ui-field" :class="{ 'is-invalid': error }">
    <label class="ui-field__label" :class="{ 'visually-hidden': hideLabel }" :for="id">
      {{ label }}<span v-if="required" class="ui-field__req" aria-hidden="true"> *</span>
    </label>
    <slot />
    <p v-if="hint" :id="hintId" class="ui-field__hint">{{ hint }}</p>
    <p v-if="error" :id="errorId" class="ui-field__error">{{ error }}</p>
  </div>
</template>

<style scoped>
.ui-field {
  margin-bottom: var(--space-4);
}
.ui-field__label {
  display: block;
  margin-bottom: var(--space-1);
  color: var(--text);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.ui-field__req {
  color: var(--danger);
}
.ui-field__hint {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.ui-field__error {
  margin: var(--space-1) 0 0;
  color: var(--danger);
  font-size: var(--text-sm);
  font-weight: 700;
}
</style>
