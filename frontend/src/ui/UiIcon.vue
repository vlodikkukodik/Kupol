<script setup lang="ts">
import { ICONS, type IconName } from './icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number | string; label?: string }>(), { size: '1.25em', label: '' })
const path = ICONS[props.name]
const px = typeof props.size === 'number' ? `${props.size}px` : props.size
</script>

<template>
  <svg
    class="ui-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    :width="px"
    :height="px"
    :role="label ? 'img' : undefined"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : 'true'"
    focusable="false"
  >
    <path :d="path" />
  </svg>
</template>

<style scoped>
.ui-icon {
  flex: none;
  vertical-align: -0.2em;
}
</style>
