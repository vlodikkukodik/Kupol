<script setup lang="ts">
import type { Inline } from '@/lib/markdown'

// Строчные элементы статьи: текст, `код`, **жирный** и ссылки. Только обычные элементы, без v-html.
defineProps<{ nodes: Inline[] }>()
</script>

<template>
  <template v-for="(n, i) in nodes" :key="i">
    <template v-if="n.t === 'text'">{{ n.v }}</template>
    <code v-else-if="n.t === 'code'">{{ n.v }}</code>
    <strong v-else-if="n.t === 'strong'"><md-inline :nodes="n.v" /></strong>
    <router-link v-else-if="n.t === 'link' && n.internal" :to="n.href">{{ n.v }}</router-link>
    <a v-else-if="n.t === 'link'" :href="n.href" target="_blank" rel="noopener noreferrer">{{ n.v }}</a>
  </template>
</template>
