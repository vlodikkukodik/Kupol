<script setup>
import Stamp from '../Stamp.vue'

defineProps({ data: { type: Object, required: true } }) // { text, tone, tilt }
</script>

<template>
  <div class="stamp-block">
    <Stamp :text="data.text" :tone="data.tone" :tilt="data.tilt" :animate="false" />
  </div>
</template>

<style scoped>
.stamp-block { margin: var(--space-4) 0; }
</style>
