<script setup lang="ts">
import type { TypeCount } from '@/api/generated/documents'

// «Папки» — типы документов как папки картотеки: цвет ярлычка у каждого типа свой.
defineProps<{ types: TypeCount[] }>()
defineEmits<{ open: [type: string] }>()
</script>

<template>
  <ul class="folders" data-testid="folders">
    <li v-for="t in types" :key="t.type">
      <button type="button" class="folder" :data-type="t.type" @click="$emit('open', t.type)">
        <span class="folder__tab" aria-hidden="true" />
        <span class="folder__name">{{ t.name }}</span>
        <span class="folder__count">{{ t.count }}</span>
      </button>
    </li>
  </ul>
</template>

<style scoped>
.folders {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(13rem, 1fr));
  gap: var(--space-5) var(--space-4);
  margin: var(--space-6) 0 0;
  padding: 0;
  list-style: none;
}
.folder {
  position: relative;
  display: grid;
  gap: var(--space-2);
  width: 100%;
  min-height: 9rem;
  padding: var(--space-4);
  border: 1px solid var(--kraft-400);
  border-radius: 0 var(--radius-3) var(--radius-2) var(--radius-2);
  background: linear-gradient(180deg, var(--kraft-200), var(--kraft-300));
  color: var(--ink-900);
  box-shadow: var(--shadow-card);
  text-align: left;
  cursor: pointer;
  transition:
    transform var(--dur-fast) var(--ease),
    box-shadow var(--dur-fast) var(--ease);
}
.folder:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-pop);
}
.folder__tab {
  position: absolute;
  top: -0.8rem;
  left: -1px;
  width: 42%;
  height: 0.85rem;
  border: 1px solid var(--kraft-400);
  border-bottom: 0;
  border-radius: var(--radius-2) var(--radius-2) 0 0;
  background: var(--kraft-300);
}
.folder__name {
  align-self: start;
  font-family: var(--font-head);
  font-size: var(--text-lg);
  font-weight: 700;
  letter-spacing: 0.08em;
  line-height: 1.2;
  text-transform: uppercase;
}
.folder__count {
  align-self: end;
  font-family: var(--font-doc);
  font-size: var(--text-3xl);
  font-weight: 700;
  line-height: 1;
}
.folder[data-type='object'] .folder__tab { background: var(--red-600); border-color: var(--red-700); }
.folder[data-type='incident'] .folder__tab { background: var(--amber-700); border-color: var(--amber-700); }
.folder[data-type='order'] .folder__tab { background: var(--blue-700); border-color: var(--blue-800); }
.folder[data-type='personnel'] .folder__tab { background: var(--green-700); border-color: var(--green-700); }
</style>
