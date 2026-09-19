<script setup>
import RichText from '../RichText.vue'

defineProps({ data: { type: Object, required: true } }) // { ordered, items: [runs] }
</script>

<template>
  <component :is="data.ordered ? 'ol' : 'ul'" class="list">
    <li v-for="(item, i) in data.items" :key="i"><RichText :runs="item" /></li>
  </component>
</template>

<style scoped>
.list { margin: 0 0 var(--space-3); padding-left: 1.6rem; overflow-wrap: anywhere; }
.list li { margin-bottom: var(--space-1); }
</style>
