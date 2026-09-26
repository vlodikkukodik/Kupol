<script setup lang="ts">
withDefaults(defineProps<{ size?: number; wordmark?: boolean }>(), { size: 38, wordmark: true })
</script>

<template>
  <span class="brand">
    <svg :width="size" :height="size" viewBox="0 0 64 64" role="img" aria-label="Vladhost">
      <defs>
        <linearGradient id="brand-g" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stop-color="#6366f1" />
          <stop offset=".5" stop-color="#8b5cf6" />
          <stop offset="1" stop-color="#ec4899" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="18" fill="url(#brand-g)" />
      <path d="M17 20l15 26 15-26" fill="none" stroke="#fff" stroke-width="7" stroke-linecap="round" stroke-linejoin="round" />
      <circle cx="49" cy="15" r="3.5" fill="#fff" opacity=".9" />
    </svg>
    <span v-if="wordmark" class="word">Vlad<span class="gradient-text">host</span></span>
  </span>
</template>

<style scoped>
.brand {
  display: inline-flex;
  align-items: center;
  gap: 11px;
  filter: drop-shadow(0 6px 18px rgba(139, 92, 246, 0.45));
}

.word {
  font-size: 21px;
  font-weight: 800;
  letter-spacing: -0.03em;
}
</style>
