<script setup lang="ts">
import type { OutIncidentTimeline } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutIncidentTimeline }>()
</script>

<template>
  <ol class="timeline">
    <li v-for="(e, i) in data.entries" :key="i" class="timeline__entry">
      <span v-if="e.time" class="timeline__time">{{ e.time }}</span>
      <span class="timeline__event"><RichRuns :runs="e.event" /></span>
    </li>
  </ol>
</template>

<style scoped>
.timeline {
  margin: var(--space-5) 0;
  padding: 0;
  list-style: none;
  border-left: 2px solid var(--border-strong);
}
.timeline__entry {
  position: relative;
  padding: var(--space-2) 0 var(--space-2) var(--space-4);
}
.timeline__entry::before {
  content: '';
  position: absolute;
  left: -0.3125rem;
  top: 0.6em;
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: var(--ink-900);
}
.timeline__time {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
  font-weight: 700;
}
.timeline__event {
  overflow-wrap: anywhere;
}
</style>
