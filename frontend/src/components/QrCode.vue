<script setup lang="ts">
import { computed } from 'vue'
import qrcode from 'qrcode-generator'

// QR-код рисуется здесь же, из матрицы модулей, — без картинок и без сети: содержимое (секрет кода из приложения)
// никуда не уходит. Чёрное на белом с полем в 4 модуля — как требует стандарт: в тёмной теме сканер иначе не прочтёт.
const props = defineProps<{ value: string; label: string }>()

const QUIET = 4 // модулей белого поля вокруг

const drawing = computed(() => {
  const qr = qrcode(0, 'M') // 0 — версия подбирается по длине данных
  qr.addData(props.value, 'Byte')
  qr.make()
  const n = qr.getModuleCount()
  // один путь на весь код: горизонтальные отрезки подряд идущих чёрных модулей
  const parts: string[] = []
  for (let r = 0; r < n; r++) {
    let c = 0
    while (c < n) {
      if (!qr.isDark(r, c)) {
        c++
        continue
      }
      const start = c
      while (c < n && qr.isDark(r, c)) c++
      parts.push(`M${start + QUIET} ${r + QUIET}h${c - start}v1h${start - c}z`)
    }
  }
  return { size: n + QUIET * 2, path: parts.join('') }
})
</script>

<template>
  <svg class="qr" role="img" :aria-label="label" :viewBox="`0 0 ${drawing.size} ${drawing.size}`" shape-rendering="crispEdges" data-testid="totp-qr">
    <rect width="100%" height="100%" fill="#fff" />
    <path :d="drawing.path" fill="#000" />
  </svg>
</template>

<style scoped>
.qr {
  display: block;
  width: min(100%, 15rem);
  height: auto;
  aspect-ratio: 1;
  border: 1px solid var(--border-strong);
}
</style>
