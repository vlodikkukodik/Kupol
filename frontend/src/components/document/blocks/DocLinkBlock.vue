<script setup lang="ts">
import type { OutDocLink } from '@/api/generated/documents'

// доступная цель: { available: true, code, slug, title, type_name, note }; недоступная: { available: false }
defineProps<{ data: OutDocLink }>()
</script>

<template>
  <RouterLink v-if="data.available && data.slug" class="link" :to="{ name: 'document', params: { ref: data.slug } }">
    <span class="link__kind">{{ data.type_name }}</span>
    <span class="link__main"><strong>{{ data.code }}</strong> {{ data.title }}</span>
    <span v-if="data.note" class="link__note">{{ data.note }}</span>
  </RouterLink>
  <div v-else class="link link--closed" role="img" aria-label="Связанный документ засекречен">
    <span class="link__kind">Связанный документ</span>
    <span class="link__main">Засекречен</span>
  </div>
</template>

<style scoped>
.link {
  display: grid;
  gap: 0.1rem;
  margin: var(--space-4) 0;
  padding: var(--space-2) var(--space-4);
  border: 2px solid var(--ink-900);
  border-left-width: 6px;
  background: var(--surface-sunken);
  color: var(--text);
  text-decoration: none;
}
a.link:hover {
  background: var(--ink-900);
  color: var(--paper-50);
}
.link__kind {
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  opacity: 0.75;
}
.link__main {
  font-size: var(--text-md);
}
.link__note {
  font-size: var(--text-sm);
  opacity: 0.85;
}
.link--closed {
  border-style: dashed;
  border-color: var(--text-muted);
  background: transparent;
  color: var(--text-muted);
  user-select: none;
}
</style>
