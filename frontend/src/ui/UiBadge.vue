<script setup lang="ts">
// Метка: статус документа, допуск, роль. Смысл передаётся текстом, цвет только помогает.
withDefaults(defineProps<{ tone?: 'neutral' | 'draft' | 'review' | 'published' | 'archived' | 'danger' | 'ink' }>(), { tone: 'neutral' })
</script>

<template>
  <span class="ui-badge" :class="`ui-badge--${tone}`"><slot /></span>
</template>

<style scoped>
.ui-badge {
  display: inline-block;
  padding: 0.15rem 0.55rem;
  border: 1.5px solid currentColor;
  border-radius: var(--radius-1);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  line-height: 1.3;
  text-transform: uppercase;
  white-space: nowrap;
}
.ui-badge--neutral { color: var(--ink-700); }
.ui-badge--draft { color: var(--ink-700); border-style: dashed; }
.ui-badge--review { color: var(--amber-700); }
.ui-badge--published { color: var(--green-700); }
.ui-badge--archived { color: var(--ink-600); background: rgb(0 0 0 / 0.05); }
.ui-badge--danger { color: var(--red-700); }
.ui-badge--ink { color: var(--paper-50); background: var(--ink-900); border-color: var(--ink-900); }
</style>
