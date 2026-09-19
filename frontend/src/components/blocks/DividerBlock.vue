<script setup>
defineProps({ data: { type: Object, required: true } }) // { style: line | stars }
</script>

<template>
  <p v-if="data.style === 'stars'" class="stars" aria-hidden="true">* * *</p>
  <hr v-else class="line">
</template>

<style scoped>
.line { margin: var(--space-5) 0; border: 0; border-top: 2px solid var(--ink); }
.stars { margin: var(--space-5) 0; text-align: center; letter-spacing: 0.6em; color: var(--ink-soft); }
</style>
