<script setup lang="ts">
import type { OutExperimentLog } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutExperimentLog }>()
</script>

<template>
  <section class="log" :aria-label="data.title || 'Журнал эксперимента'">
    <h3 v-if="data.title" class="log__title">{{ data.title }}</h3>
    <ol class="entries">
      <li v-for="(e, i) in data.entries" :key="i" class="entry">
        <div v-if="e.date || e.participants?.length" class="entry__meta">
          <span v-if="e.date" class="entry__date">{{ e.date }}</span>
          <span v-if="e.participants?.length">Участники: {{ e.participants.join(', ') }}</span>
        </div>
        <p class="entry__text"><RichRuns :runs="e.text" /></p>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.log {
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--ink-900);
  background: var(--surface-sunken);
}
.log__title {
  margin: 0 0 var(--space-3);
  font-size: var(--text-lg);
}
.entries {
  margin: 0;
  padding: 0;
  list-style: none;
}
.entry {
  padding: var(--space-2) 0;
  border-top: 1px dashed var(--border-strong);
}
.entry:first-child {
  border-top: 0;
}
.entry__meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.entry__date {
  color: var(--text);
  font-weight: 700;
}
.entry__text {
  margin: var(--space-1) 0 0;
  white-space: pre-line;
  overflow-wrap: anywhere;
}
</style>
