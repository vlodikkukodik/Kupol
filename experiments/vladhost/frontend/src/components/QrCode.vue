<script setup lang="ts">
// QR-код строки (ссылка otpauth:// для приложения-аутентификатора). Рисуется SVG-путём на белой подложке:
// камеры телефонов плохо читают светлый код на тёмном фоне.
import { encode } from 'uqr'
import { computed } from 'vue'

const props = withDefaults(defineProps<{ value: string; size?: number; label: string }>(), { size: 196 })

const qr = computed(() => encode(props.value, { border: 2, ecc: 'M' }))
const path = computed(() => {
  let d = ''
  qr.value.data.forEach((row, y) =>
    row.forEach((on, x) => {
      if (on) d += `M${x} ${y}h1v1h-1z`
    }),
  )
  return d
})
</script>

<template>
  <svg
    class="qr"
    role="img"
    :aria-label="label"
    :width="size"
    :height="size"
    :viewBox="`0 0 ${qr.size} ${qr.size}`"
    shape-rendering="crispEdges"
  >
    <rect :width="qr.size" :height="qr.size" fill="#fff" />
    <path :d="path" fill="#000" />
  </svg>
</template>

<style scoped>
.qr {
  display: block;
  border-radius: 12px;
  max-width: 100%;
  height: auto;
}
</style>
