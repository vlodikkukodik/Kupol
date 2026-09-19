<script setup lang="ts">
// Обёртка для широких таблиц: прокручивается внутри себя (страница не «ездит» вбок), доступна с клавиатуры,
// оформление — как у реестра: двойная линия под шапкой, пунктир между строками.
defineProps<{ label: string }>()
</script>

<template>
  <div class="ui-table" tabindex="0" role="region" :aria-label="label">
    <slot />
  </div>
</template>

<style scoped>
.ui-table {
  overflow-x: auto;
}
.ui-table :deep(table) {
  width: 100%;
  border-collapse: collapse;
}
.ui-table :deep(thead th) {
  padding: var(--space-3);
  border-bottom: 2px solid var(--ink-900);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-align: left;
  text-transform: uppercase;
  white-space: nowrap;
}
.ui-table :deep(td),
.ui-table :deep(tbody th) {
  padding: var(--space-3);
  border-bottom: 1px dashed var(--border-strong);
  font-weight: inherit;
  text-align: left;
  vertical-align: top;
}
.ui-table :deep(tbody tr:hover) {
  background: rgb(255 255 255 / 0.4);
}
</style>
