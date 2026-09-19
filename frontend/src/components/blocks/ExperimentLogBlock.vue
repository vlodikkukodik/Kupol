<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { title, entries: [{date, participants, text}] }
</script>

<template>
  <section class="log" :aria-label="data.title || 'Журнал эксперимента'">
    <h3 v-if="data.title" class="title">{{ data.title }}</h3>
    <ol class="entries">
      <li v-for="(e, i) in data.entries" :key="i" class="entry">
        <div v-if="e.date || (e.participants && e.participants.length)" class="meta">
          <span v-if="e.date" class="date">{{ e.date }}</span>
          <span v-if="e.participants && e.participants.length" class="who">Участники: {{ e.participants.join(', ') }}</span>
        </div>
        <p class="text"><RichText :runs="e.text" /></p>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.log { margin: var(--space-4) 0; padding: var(--space-3) var(--space-4); border: 1px solid var(--ink); background: var(--paper-shade); }
.title { margin: 0 0 var(--space-3); font-size: 1.1rem; }
.entries { margin: 0; padding: 0; list-style: none; }
.entry { padding: var(--space-2) 0; border-top: 1px dashed var(--ink-soft); }
.entry:first-child { border-top: 0; }
.meta { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-4); font-size: 0.85rem; color: var(--ink-soft); }
.date { font-weight: 700; color: var(--ink); }
.text { margin: var(--space-1) 0 0; white-space: pre-line; overflow-wrap: anywhere; }
</style>
