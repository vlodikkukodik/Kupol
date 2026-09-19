<script setup>
defineProps({ data: { type: Object, required: true } }) // { number, title }
</script>

<template>
  <h2 class="appendix">
    <span class="mark">Приложение<template v-if="data.number"> № {{ data.number }}</template></span>
    <span class="title">{{ data.title }}</span>
  </h2>
</template>

<style scoped>
.appendix {
  margin: var(--space-6) 0 var(--space-3);
  padding: var(--space-2) 0;
  border-top: 3px double var(--ink);
  border-bottom: 1px solid var(--ink);
  font-size: 1.1rem;
}
.mark { display: block; font-size: 0.85rem; color: var(--ink-soft); }
</style>
