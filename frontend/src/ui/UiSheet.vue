<script setup lang="ts">
// Лист бумаги на столе: светлая поверхность с текстурой и тенью. Всё, что «лежит на столе» — карточки, документы,
// формы — делается листами; сам стол — тёмный фон страницы.
withDefaults(
  defineProps<{
    as?: string
    /** Ширина: обычный лист или широкий (реестры, картотека) */
    wide?: boolean
    /** Загнутый уголок */
    fold?: boolean
    padded?: boolean
  }>(),
  { as: 'div', wide: false, fold: false, padded: true },
)
</script>

<template>
  <component :is="as" class="ui-sheet" :class="{ 'ui-sheet--wide': wide, 'ui-sheet--fold': fold, 'ui-sheet--padded': padded }">
    <slot />
  </component>
</template>

<style scoped>
.ui-sheet {
  position: relative;
  max-width: var(--sheet-max);
  margin-inline: auto;
  border: 1px solid var(--paper-400);
  border-radius: var(--radius-2);
  background: var(--texture-paper), var(--surface);
  color: var(--text);
  box-shadow: var(--shadow-sheet);
}
.ui-sheet--wide {
  max-width: var(--page-max);
}
.ui-sheet--padded {
  padding: var(--space-6);
}
.ui-sheet--fold::after {
  content: '';
  position: absolute;
  top: -1px;
  right: -1px;
  width: 2.2rem;
  height: 2.2rem;
  border-bottom-left-radius: var(--radius-2);
  background: linear-gradient(225deg, var(--bg) 50%, var(--paper-300) 50%);
  box-shadow: -2px 2px 4px rgb(0 0 0 / 0.25);
}
@media (max-width: 40rem) {
  .ui-sheet--padded {
    padding: var(--space-4);
  }
}
</style>
