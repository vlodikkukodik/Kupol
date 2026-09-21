<script setup lang="ts">
import { reactive } from 'vue'
import type { Node as PMNode } from '@tiptap/pm/model'
import { FIELDS, fieldLabel, fieldOptions, type FieldSpec } from '@/editor/fields'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import LinesField from './LinesField.vue'

// Поля из шапки блока: всё, что в блоке не набирается текстом (номер, дата, вид, наклон штампа…), — обычные поля ввода.
// Значения лежат в атрибутах узла; поле «Наклон» набирается как число, поэтому его ввод (например, «-» в начале)
// держится в черновике, пока не станет числом.
const props = defineProps<{ node: PMNode; disabled: boolean }>()
const emit = defineEmits<{ update: [attrs: Record<string, unknown>] }>()

const drafts = reactive<Record<string, string>>({})

const specs = (): FieldSpec[] => FIELDS[props.node.type.name] ?? []

function stored(spec: FieldSpec): string {
  const v: unknown = props.node.attrs[spec.key]
  return v === null || v === undefined ? '' : String(v)
}

function shown(spec: FieldSpec): string {
  return drafts[spec.key] ?? stored(spec)
}

function commit(spec: FieldSpec, raw: string) {
  let value: unknown = raw
  if (spec.key === 'ordered') value = raw === 'true'
  else if (spec.key === 'depth') value = Math.min(3, Math.max(1, Number(raw) || 2))
  else if (spec.kind === 'number') {
    drafts[spec.key] = raw
    if (raw.trim() === '') value = 0
    else if (!Number.isFinite(Number(raw))) return
    else value = Math.min(spec.max, Math.max(spec.min, Math.trunc(Number(raw))))
  }
  emit('update', { [spec.key]: value })
}

function settle(spec: FieldSpec) {
  delete drafts[spec.key]
}
</script>

<template>
  <div v-if="specs().length" class="pm-fields" contenteditable="false">
    <UiField v-for="spec in specs()" :key="spec.key" :label="fieldLabel(node.type.name, spec.key)" :hint="spec.kind === 'lines' ? $t('editor.linesHint') : ''" :class="{ 'pm-fields__wide': spec.wide }">
      <UiSelect v-if="spec.kind === 'select'" :model-value="stored(spec)" :options="fieldOptions(node.type.name, spec)" :disabled="disabled" @update:model-value="commit(spec, $event)" />
      <LinesField
        v-else-if="spec.kind === 'lines'"
        :model-value="(node.attrs[spec.key] as string[]) ?? []"
        :disabled="disabled"
        @update:model-value="emit('update', { [spec.key]: $event })"
      />
      <UiInput
        v-else-if="spec.kind === 'number'"
        type="number"
        inputmode="numeric"
        :min="spec.min"
        :max="spec.max"
        :model-value="shown(spec)"
        :disabled="disabled"
        @update:model-value="commit(spec, $event)"
        @change="settle(spec)"
      />
      <UiInput v-else :model-value="stored(spec)" :maxlength="spec.maxLength" :disabled="disabled" @update:model-value="commit(spec, $event)" />
    </UiField>
  </div>
</template>

<style scoped>
.pm-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr));
  gap: var(--space-2) var(--space-3);
  margin-bottom: var(--space-3);
  font-family: var(--font-ui);
}
.pm-fields :deep(.ui-field) {
  margin-bottom: 0;
}
.pm-fields__wide {
  grid-column: 1 / -1;
}
</style>
