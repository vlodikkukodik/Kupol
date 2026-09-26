<script setup lang="ts">
import type { Locale } from '@/i18n'

// Круглый флаг языка (рисуется SVG: эмодзи-флаги не отображаются в Windows).
defineProps<{ locale: Locale; size?: number }>()
</script>

<template>
  <svg :width="size ?? 18" :height="size ?? 18" viewBox="0 0 20 20" aria-hidden="true" class="flag">
    <defs>
      <clipPath :id="`flag-${locale}`"><circle cx="10" cy="10" r="10" /></clipPath>
    </defs>
    <g :clip-path="`url(#flag-${locale})`">
      <template v-if="locale === 'ru'">
        <rect width="20" height="7" fill="#f8fafc" />
        <rect y="7" width="20" height="6.5" fill="#2563eb" />
        <rect y="13.5" width="20" height="6.5" fill="#ef4444" />
      </template>
      <template v-else>
        <rect width="7" height="20" fill="#16a34a" />
        <rect x="7" width="6.5" height="20" fill="#f8fafc" />
        <rect x="13.5" width="6.5" height="20" fill="#dc2626" />
      </template>
    </g>
    <circle cx="10" cy="10" r="9.6" fill="none" stroke="rgba(255,255,255,.35)" stroke-width=".8" />
  </svg>
</template>

<style scoped>
.flag {
  flex: none;
  filter: drop-shadow(0 1px 3px rgba(0, 0, 0, 0.4));
}
</style>
