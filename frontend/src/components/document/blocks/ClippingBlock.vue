<script setup lang="ts">
import { computed } from 'vue'
import type { OutClipping } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

const props = defineProps<{ data: OutClipping }>()

const label = computed(() => ({ newspaper: 'Газетная вырезка', handwritten: 'Рукописная заметка', transcript: 'Расшифровка записи' })[props.data.kind] || 'Материал')
</script>

<template>
  <figure class="clip" :class="`clip--${data.kind}`" :aria-label="label">
    <figcaption class="clip__head">
      <span>{{ label }}</span>
      <span v-if="data.source">{{ data.source }}</span>
      <span v-if="data.date" class="clip__date">{{ data.date }}</span>
    </figcaption>
    <h3 v-if="data.title" class="clip__title">{{ data.title }}</h3>
    <template v-if="data.kind === 'transcript'">
      <p v-for="(line, i) in data.lines" :key="i" class="line">
        <strong v-if="line.speaker" class="speaker">{{ line.speaker }}:</strong>
        <RichRuns :runs="line.text" />
      </p>
    </template>
    <template v-else>
      <p v-for="(para, i) in data.paragraphs" :key="i" class="para"><RichRuns :runs="para" /></p>
    </template>
  </figure>
</template>

<style scoped>
.clip {
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--paper-400);
  background: var(--kraft-200);
}
.clip__head {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.clip__date {
  margin-left: auto;
}
.clip__title {
  margin: var(--space-2) 0;
  font-size: var(--text-lg);
}
.para,
.line {
  margin: 0 0 var(--space-2);
  white-space: pre-line;
  overflow-wrap: anywhere;
}
.clip--newspaper {
  border-top: 3px double var(--ink-900);
  border-bottom: 3px double var(--ink-900);
  font-size: var(--text-sm);
}
.clip--handwritten .para {
  font-style: italic;
  transform: rotate(-0.4deg);
}
.clip--transcript {
  background: var(--surface-sunken);
  font-size: var(--text-sm);
}
.speaker {
  margin-right: 0.4ch;
}
</style>
