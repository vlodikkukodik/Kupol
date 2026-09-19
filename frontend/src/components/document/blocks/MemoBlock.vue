<script setup lang="ts">
import { computed } from 'vue'
import type { OutMemo } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

const props = defineProps<{ data: OutMemo }>()

const heading = computed(() => ({ memo: 'Служебная записка', order: 'Приказ', letter: 'Письмо' })[props.data.kind] || 'Документ')
</script>

<template>
  <section class="blank" :class="`blank--${data.kind}`" :aria-label="heading">
    <header class="blank__top">
      <span class="blank__kind">{{ heading }}</span>
      <span v-if="data.number">№ {{ data.number }}</span>
      <span v-if="data.date" class="blank__date">{{ data.date }}</span>
    </header>
    <dl v-if="data.from || data.to?.length || data.subject" class="route">
      <div v-if="data.from"><dt>От</dt><dd>{{ data.from }}</dd></div>
      <div v-if="data.to?.length"><dt>Кому</dt><dd>{{ data.to.join('; ') }}</dd></div>
      <div v-if="data.subject"><dt>Тема</dt><dd>{{ data.subject }}</dd></div>
    </dl>
    <p v-for="(para, i) in data.body" :key="i" class="para"><RichRuns :runs="para" /></p>
    <p v-if="data.signature" class="signature">{{ data.signature }}</p>
  </section>
</template>

<style scoped>
.blank {
  margin: var(--space-5) 0;
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--paper-400);
  background: var(--paper-50);
  box-shadow: 3px 3px 0 var(--paper-300);
}
.blank__top {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-4);
  margin-bottom: var(--space-3);
  padding-bottom: var(--space-2);
  border-bottom: 2px solid var(--ink-900);
}
.blank__kind {
  font-family: var(--font-head);
  font-size: var(--text-lg);
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.blank__date {
  margin-left: auto;
}
.route {
  margin: 0 0 var(--space-3);
}
.route div {
  display: flex;
  gap: var(--space-3);
  padding: 0.1rem 0;
}
.route dt {
  flex: none;
  width: 4rem;
  color: var(--text-muted);
}
.route dd {
  margin: 0;
  font-weight: 700;
  overflow-wrap: anywhere;
}
.para {
  white-space: pre-line;
  overflow-wrap: anywhere;
}
.signature {
  margin: var(--space-4) 0 0;
  text-align: right;
  font-style: italic;
}
</style>
