<script setup lang="ts">
import { computed, inject, ref } from 'vue'
import { NodeViewContent, NodeViewWrapper, nodeViewProps } from '@tiptap/vue-3'
import { EDITABLE } from '@/editor/context'
import { NODE_LABELS } from '@/editor/fields'
import UiStamp from '@/ui/UiStamp.vue'
import BlockFields from './BlockFields.vue'
import BlockHead from './BlockHead.vue'

// Представление любого блока с полями в шапке и (у большинства) с текстом внутри. Один компонент на все виды:
// что за поля — решает перечень FIELDS, что вокруг — CSS по имени узла (`pm-<имя>`).
const props = defineProps(nodeViewProps)

const editable = inject(EDITABLE, ref(true))
const name = computed(() => props.node.type.name)
const label = computed(() => NODE_LABELS[name.value] ?? name.value)
const level = computed<number | null>(() => (typeof props.node.attrs.level === 'number' ? props.node.attrs.level : null))
/** Вложенный блок (запись журнала, реплика) — без своей шапки с видом и допуском: они у объемлющего блока. */
const nested = computed(() => name.value === 'logEntry' || name.value === 'clipLine')
const hasText = computed(() => !props.node.type.isAtom)
</script>

<template>
  <NodeViewWrapper
    class="pm-block"
    :class="[`pm-${name}`, { 'pm-block--nested': nested }]"
    role="group"
    :aria-label="label"
    :data-block-id="node.attrs.blockId || undefined"
    :data-block-level="level ?? undefined"
    :data-depth="node.attrs.depth"
    :data-kind="node.attrs.kind"
    :data-ordered="node.attrs.ordered ? 'true' : 'false'"
  >
    <BlockHead v-if="!nested" :name="name" :level="level" />

    <p v-if="name === 'dossierHeader'" class="pm-note" contenteditable="false">
      Реквизиты досье (шифр, класс опасности, отдел и другие свойства) выводятся сами — из свойств документа.
    </p>
    <p v-else-if="name === 'unknownBlock'" class="pm-note" contenteditable="false">
      Блок вида «{{ node.attrs.kind }}» этот редактор не показывает. Он сохранится без изменений.
    </p>

    <BlockFields :node="node" :disabled="!editable" @update="updateAttributes($event)" />

    <div v-if="name === 'stamp'" class="pm-stamp-preview" contenteditable="false" aria-hidden="true">
      <UiStamp :text="node.attrs.text || 'Штамп'" :tone="node.attrs.tone === 'ink' ? 'ink' : 'red'" :tilt="Number(node.attrs.tilt) || 0" :animate="false" />
    </div>

    <p v-if="name === 'divider'" class="pm-divider-preview" contenteditable="false" aria-hidden="true" :data-style="node.attrs.style" />
    <p v-else-if="name === 'pageBreak'" class="pm-page-preview" contenteditable="false" aria-hidden="true">— страница {{ node.attrs.number }} —</p>

    <NodeViewContent v-if="hasText" class="pm-content" :role="name === 'list' ? 'list' : undefined" />
  </NodeViewWrapper>
</template>
