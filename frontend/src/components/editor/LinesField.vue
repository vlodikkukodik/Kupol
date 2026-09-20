<script setup lang="ts">
import { ref, watch } from 'vue'
import { useField } from '@/ui/field'
import '@/ui/controls.css'

// Список строк в одном поле: по значению на строке (имя с запятой внутри допустимо). Пустые строки и пробелы по краям
// в значение не попадают, но пока человек печатает, в поле остаётся ровно то, что он набрал (в том числе пустая строка
// в конце): иначе нельзя было бы нажать Enter, чтобы начать следующую.
const props = defineProps<{ modelValue: string[]; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const field = useField()

const parse = (text: string): string[] =>
  text
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)

const draft = ref(props.modelValue.join('\n'))

// Значение изменили снаружи (отмена правки, откат версии): показываем его, если оно не то, что уже набрано.
watch(
  () => props.modelValue,
  (value) => {
    if (JSON.stringify(value) !== JSON.stringify(parse(draft.value))) draft.value = value.join('\n')
  },
)

function onInput(event: Event) {
  draft.value = (event.target as HTMLTextAreaElement).value
  emit('update:modelValue', parse(draft.value))
}
</script>

<template>
  <textarea
    :id="field?.id"
    class="ui-control pm-lines"
    rows="2"
    spellcheck="false"
    :value="draft"
    :disabled="disabled"
    :aria-describedby="field?.describedBy"
    @input="onInput"
    @blur="draft = modelValue.join('\n')"
  />
</template>

<style scoped>
.pm-lines {
  min-height: 3.5rem;
}
</style>
