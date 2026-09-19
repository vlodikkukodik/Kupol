<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { text: runs, source }
</script>

<template>
  <figure class="quote">
    <blockquote><RichText :runs="data.text" /></blockquote>
    <figcaption v-if="data.source">— {{ data.source }}</figcaption>
  </figure>
</template>

<style scoped>
.quote { margin: var(--space-4) 0; padding-left: var(--space-4); border-left: 4px solid var(--rule); }
blockquote { margin: 0; font-style: italic; white-space: pre-line; overflow-wrap: anywhere; }
figcaption { margin-top: var(--space-2); font-size: 0.9rem; color: var(--ink-soft); }
</style>
