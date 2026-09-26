<script setup lang="ts">
import { computed, inject, ref } from 'vue'
import { NodeViewContent, NodeViewWrapper, nodeViewProps } from '@tiptap/vue-3'
import { EDITABLE } from '@/editor/context'
import BlockFields from './BlockFields.vue'
import BlockHead from './BlockHead.vue'

// Таблица: подпись — поле в шапке, ячейки набираются прямо в таблице. Первая строка — названия столбцов.
const props = defineProps(nodeViewProps)
const editable = inject(EDITABLE, ref(true))
const level = computed<number | null>(() => (typeof props.node.attrs.level === 'number' ? props.node.attrs.level : null))
</script>

<template>
  <NodeViewWrapper class="pm-block pm-table" role="group" :aria-label="$t('editor.node.table')" :data-block-id="node.attrs.blockId || undefined" :data-block-level="level ?? undefined">
    <BlockHead name="table" :level="level" />
    <BlockFields :node="node" :disabled="!editable" @update="updateAttributes($event)" />
    <div class="pm-table__scroll">
      <table class="pm-table__grid">
        <NodeViewContent as="tbody" />
      </table>
    </div>
  </NodeViewWrapper>
</template>
