<script setup lang="ts">
// Заголовок страницы на столе: гриф-надпись над названием, название, пояснение и действия справа.
defineProps<{ title: string; kicker?: string }>()
</script>

<template>
  <header class="ui-page-header">
    <div class="ui-page-header__main">
      <p v-if="kicker" class="ui-page-header__kicker">{{ kicker }}</p>
      <h1 class="ui-page-header__title">{{ title }}</h1>
      <div v-if="$slots.default" class="ui-page-header__lead"><slot /></div>
    </div>
    <div v-if="$slots.actions" class="ui-page-header__actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.ui-page-header {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-4);
  margin-top: var(--space-5);
  margin-bottom: var(--space-5);
  padding-bottom: var(--space-4);
  border-bottom: 2px solid var(--line-on-bg);
  color: var(--on-bg);
}
.ui-page-header__main {
  min-width: 0;
}
.ui-page-header__kicker {
  margin: 0 0 var(--space-1);
  color: var(--on-bg-muted);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: 0.2em;
  text-transform: uppercase;
}
.ui-page-header__title {
  margin: 0;
}
.ui-page-header__lead {
  max-width: 42rem;
  margin-top: var(--space-2);
  color: var(--on-bg-muted);
}
.ui-page-header__lead > :last-child {
  margin-bottom: 0;
}
.ui-page-header__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
</style>
