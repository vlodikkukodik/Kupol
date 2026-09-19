<script setup>
defineProps({ data: { type: Object, required: true } }) // { number }
</script>

<template>
  <p class="page">— стр.<template v-if="data.number"> {{ data.number }}</template> —</p>
</template>

<style scoped>
.page {
  margin: var(--space-5) 0;
  padding-top: var(--space-2);
  border-top: 1px dashed var(--ink-soft);
  text-align: center;
  font-size: 0.85rem;
  letter-spacing: 0.1em;
  color: var(--ink-soft);
}
</style>
