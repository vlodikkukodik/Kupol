<script setup lang="ts">
import type { OutList } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

defineProps<{ data: OutList }>()
</script>

<template>
  <component :is="data.ordered ? 'ol' : 'ul'" class="list">
    <li v-for="(item, i) in data.items" :key="i"><RichRuns :runs="item" /></li>
  </component>
</template>

<style scoped>
.list {
  margin: 0 0 var(--space-4);
  padding-left: 2rem;
  overflow-wrap: anywhere;
}
.list li {
  margin-bottom: var(--space-1);
}
ul.list {
  list-style: '– ';
}
</style>
