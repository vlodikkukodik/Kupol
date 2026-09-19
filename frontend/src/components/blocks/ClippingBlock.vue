<script setup>
import { computed } from 'vue'
import RichText from '../RichText.vue'

const props = defineProps({ data: { type: Object, required: true } })
// { kind: newspaper | handwritten | transcript, title, source, date, paragraphs: [runs], lines: [{speaker, text}] }

const label = computed(
  () => ({ newspaper: 'Газетная вырезка', handwritten: 'Рукописная заметка', transcript: 'Расшифровка записи' })[props.data.kind] || 'Материал',
)
</script>

<template>
  <figure class="clip" :class="`clip--${data.kind}`" :aria-label="label">
    <figcaption class="head">
      <span class="label">{{ label }}</span>
      <span v-if="data.source" class="source">{{ data.source }}</span>
      <span v-if="data.date" class="date">{{ data.date }}</span>
    </figcaption>
    <h3 v-if="data.title" class="title">{{ data.title }}</h3>
    <template v-if="data.kind === 'transcript'">
      <p v-for="(line, i) in data.lines" :key="i" class="line">
        <strong v-if="line.speaker" class="speaker">{{ line.speaker }}:</strong>
        <RichText :runs="line.text" />
      </p>
    </template>
    <template v-else>
      <p v-for="(para, i) in data.paragraphs" :key="i" class="para"><RichText :runs="para" /></p>
    </template>
  </figure>
</template>

<style scoped>
.clip { margin: var(--space-4) 0; padding: var(--space-3) var(--space-4); background: #efe6cb; border: 1px solid var(--rule); }
.head { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-3); font-size: 0.8rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--ink-soft); }
.date { margin-left: auto; }
.title { margin: var(--space-2) 0; font-size: 1.15rem; }
.para, .line { margin: 0 0 var(--space-2); white-space: pre-line; overflow-wrap: anywhere; }
.clip--newspaper { column-gap: var(--space-5); border-top: 3px double var(--ink); border-bottom: 3px double var(--ink); font-size: 0.95rem; }
.clip--handwritten .para { font-style: italic; transform: rotate(-0.4deg); }
.clip--transcript { background: var(--paper-shade); font-size: 0.95rem; }
.speaker { margin-right: 0.4ch; }
</style>
