<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { mark, text: runs }
</script>

<template>
  <p class="footnote"><sup>{{ data.mark }}</sup> <RichText :runs="data.text" /></p>
</template>

<style scoped>
.footnote { margin: var(--space-2) 0; font-size: 0.85rem; color: var(--ink-soft); overflow-wrap: anywhere; }
</style>
