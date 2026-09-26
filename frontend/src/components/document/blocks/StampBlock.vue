<script setup lang="ts">
import UiStamp from '@/ui/UiStamp.vue'
import type { OutStamp } from '@/api/generated/documents'

defineProps<{ data: OutStamp }>()
</script>

<template>
  <div class="stamp-block">
    <UiStamp :text="data.text" :tone="data.tone === 'ink' ? 'ink' : 'red'" :tilt="data.tilt" :animate="false" />
  </div>
</template>

<style scoped>
.stamp-block {
  margin: var(--space-5) 0;
}
</style>
