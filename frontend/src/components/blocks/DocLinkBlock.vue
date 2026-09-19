<script setup>
defineProps({ data: { type: Object, required: true } })
// доступная цель: { available: true, code, slug, title, type_name, note }; недоступная: { available: false }
</script>

<template>
  <RouterLink v-if="data.available" class="link" :to="{ name: 'document', params: { ref: data.slug } }">
    <span class="kind">{{ data.type_name }}</span>
    <span class="main"><strong class="code">{{ data.code }}</strong> {{ data.title }}</span>
    <span v-if="data.note" class="note">{{ data.note }}</span>
  </RouterLink>
  <div v-else class="link link--closed" role="img" aria-label="Связанный документ засекречен">
    <span class="kind">Связанный документ</span>
    <span class="main">Засекречен</span>
  </div>
</template>

<style scoped>
.link {
  display: grid;
  gap: 0.1rem;
  margin: var(--space-3) 0;
  padding: var(--space-2) var(--space-4);
  border: 2px solid var(--ink);
  border-left-width: 6px;
  background: var(--paper-shade);
  color: var(--ink);
  text-decoration: none;
}
a.link:hover { background: var(--ink); color: var(--paper); }
.kind { font-family: var(--font-head); font-size: 0.8rem; letter-spacing: 0.12em; text-transform: uppercase; opacity: 0.75; }
.main { font-size: 1.05rem; }
.note { font-size: 0.9rem; opacity: 0.85; }
.link--closed { border-style: dashed; border-color: var(--ink-soft); color: var(--ink-soft); background: transparent; user-select: none; }
</style>
