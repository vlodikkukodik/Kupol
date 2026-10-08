<script setup lang="ts">
import type { OutRouting } from '@/api/generated/documents'

defineProps<{ data: OutRouting }>()
const DECISIONS = ['approved', 'rejected', 'noted', 'pending']
const decisionKey = (d: string) => `doc.block.routingDecision.${DECISIONS.includes(d) ? d : 'pending'}`
</script>

<template>
  <ul class="routing">
    <li v-for="(e, i) in data.entries" :key="i">
      <span class="routing__who">{{ e.who }}</span>
      <span class="routing__decision" :class="`routing__decision--${e.decision}`">{{ $t(decisionKey(e.decision)) }}</span>
    </li>
  </ul>
</template>

<style scoped>
.routing {
  margin: var(--space-5) 0;
  padding: 0;
  list-style: none;
}
.routing li {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-1) 0;
  border-top: 1px solid var(--border-strong);
}
.routing li:first-child {
  border-top: 0;
}
.routing__who {
  overflow-wrap: anywhere;
}
.routing__decision {
  flex: none;
  font-size: var(--text-sm);
  font-weight: 700;
  text-transform: uppercase;
}
.routing__decision--rejected {
  color: var(--danger);
}
</style>
