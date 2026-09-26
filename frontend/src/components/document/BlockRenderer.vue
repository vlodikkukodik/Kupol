<script setup lang="ts">
import { computed, type Component } from 'vue'
import type { DocBlock } from '@/api/blocks'
import AudioBlock from './blocks/AudioBlock.vue'
import AppendixBlock from './blocks/AppendixBlock.vue'
import ClippingBlock from './blocks/ClippingBlock.vue'
import ContainmentProcedureBlock from './blocks/ContainmentProcedureBlock.vue'
import DirectiveBlock from './blocks/DirectiveBlock.vue'
import DividerBlock from './blocks/DividerBlock.vue'
import DocLinkBlock from './blocks/DocLinkBlock.vue'
import DossierHeaderBlock from './blocks/DossierHeaderBlock.vue'
import ExperimentLogBlock from './blocks/ExperimentLogBlock.vue'
import FootnoteBlock from './blocks/FootnoteBlock.vue'
import HeadingBlock from './blocks/HeadingBlock.vue'
import HypothesisBlock from './blocks/HypothesisBlock.vue'
import IncidentTimelineBlock from './blocks/IncidentTimelineBlock.vue'
import ImageBlock from './blocks/ImageBlock.vue'
import ListBlock from './blocks/ListBlock.vue'
import MemoBlock from './blocks/MemoBlock.vue'
import PageBlock from './blocks/PageBlock.vue'
import ParagraphBlock from './blocks/ParagraphBlock.vue'
import PersonnelRecordBlock from './blocks/PersonnelRecordBlock.vue'
import QABlock from './blocks/QABlock.vue'
import QuoteBlock from './blocks/QuoteBlock.vue'
import RedactedPlate from './blocks/RedactedPlate.vue'
import RosterBlock from './blocks/RosterBlock.vue'
import RoutingBlock from './blocks/RoutingBlock.vue'
import StampBlock from './blocks/StampBlock.vue'
import TableBlock from './blocks/TableBlock.vue'

// Соответствие видов блока из ответа сервера компонентам. redacted — метка закрытого блока.
const KINDS: Record<DocBlock['type'], Component> = {
  heading: HeadingBlock,
  paragraph: ParagraphBlock,
  list: ListBlock,
  quote: QuoteBlock,
  dossier_header: DossierHeaderBlock,
  experiment_log: ExperimentLogBlock,
  stamp: StampBlock,
  memo: MemoBlock,
  clipping: ClippingBlock,
  table: TableBlock,
  doc_link: DocLinkBlock,
  divider: DividerBlock,
  page: PageBlock,
  footnote: FootnoteBlock,
  appendix: AppendixBlock,
  containment_procedure: ContainmentProcedureBlock,
  directive: DirectiveBlock,
  incident_timeline: IncidentTimelineBlock,
  personnel_record: PersonnelRecordBlock,
  roster: RosterBlock,
  hypothesis: HypothesisBlock,
  qa: QABlock,
  routing: RoutingBlock,
  image: ImageBlock,
  audio: AudioBlock,
  redacted: RedactedPlate,
}

const props = defineProps<{ block: DocBlock }>()

// Только собственные ключи: вид вроде «constructor» не должен превратиться в компонент из прототипа.
const kind = computed(() => (Object.hasOwn(KINDS, props.block.type) ? KINDS[props.block.type] : null))
</script>

<template>
  <component :is="kind" v-if="kind" :data="block.data" />
  <p v-else class="unknown" role="note">{{ $t('doc.unknownBlock') }}</p>
</template>

<style scoped>
.unknown {
  padding: var(--space-2) var(--space-3);
  border: 1px dashed var(--text-muted);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
