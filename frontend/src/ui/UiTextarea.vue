<script setup lang="ts">
import { useField } from './field'
import './controls.css'

// Многострочное поле. Подпись, подсказка и ошибка — у обрамляющего UiField (он же даёт идентификатор и aria-связи).
const model = defineModel<string>({ default: '' })
withDefaults(defineProps<{ rows?: number; maxlength?: number; placeholder?: string; disabled?: boolean; name?: string }>(), {
  rows: 4,
  maxlength: undefined,
  placeholder: undefined,
  name: undefined,
})
defineEmits<{ change: [event: Event] }>()
const field = useField()
</script>

<template>
  <textarea
    :id="field?.id"
    class="ui-control"
    :value="model"
    :rows="rows"
    :maxlength="maxlength"
    :placeholder="placeholder"
    :disabled="disabled"
    :name="name"
    :required="field?.required"
    :aria-invalid="field?.invalid ? 'true' : undefined"
    :aria-describedby="field?.describedBy"
    @input="model = ($event.target as HTMLTextAreaElement).value"
    @change="$emit('change', $event)"
  />
</template>
