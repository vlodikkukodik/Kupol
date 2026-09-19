<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { text: runs }
</script>

<template>
  <p class="paragraph"><RichText :runs="data.text" /></p>
</template>

<style scoped>
.paragraph { white-space: pre-line; overflow-wrap: anywhere; }
</style>
