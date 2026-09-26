<script setup lang="ts">
import { computed } from 'vue'

// Круглый аватар с инициалом. Цвет стабильно выбирается по имени, поэтому у человека он всегда один и тот же.
const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 36 })

const gradients = [
  'linear-gradient(135deg,#6366f1,#ec4899)',
  'linear-gradient(135deg,#06b6d4,#3b82f6)',
  'linear-gradient(135deg,#10b981,#22d3ee)',
  'linear-gradient(135deg,#f59e0b,#f43f5e)',
  'linear-gradient(135deg,#8b5cf6,#d946ef)',
]

const bg = computed(() => {
  let h = 0
  for (const ch of props.name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return gradients[h % gradients.length]
})
const initial = computed(() => (props.name.trim()[0] ?? '?').toUpperCase())
</script>

<template>
  <span class="avatar" :style="{ width: `${size}px`, height: `${size}px`, fontSize: `${size * 0.42}px`, background: bg }">
    {{ initial }}
  </span>
</template>

<style scoped>
.avatar {
  display: inline-grid;
  place-items: center;
  flex: none;
  border-radius: 50%;
  color: #fff;
  font-weight: 800;
  box-shadow:
    0 6px 18px -6px rgba(139, 92, 246, 0.7),
    0 0 0 2px rgba(255, 255, 255, 0.18) inset;
}
</style>
