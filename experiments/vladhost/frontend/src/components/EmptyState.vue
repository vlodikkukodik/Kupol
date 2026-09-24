<script setup lang="ts">
defineProps<{ title: string; hint?: string }>()
</script>

<template>
  <div class="empty">
    <svg class="art" viewBox="0 0 220 150" aria-hidden="true">
      <defs>
        <linearGradient id="es-a" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stop-color="#6366f1" />
          <stop offset="1" stop-color="#ec4899" />
        </linearGradient>
        <linearGradient id="es-b" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stop-color="#22d3ee" stop-opacity=".9" />
          <stop offset="1" stop-color="#6366f1" stop-opacity=".2" />
        </linearGradient>
      </defs>
      <ellipse cx="110" cy="132" rx="70" ry="9" fill="rgba(139,92,246,.22)" />
      <g class="cloud">
        <path
          d="M64 104a26 26 0 0 1 4-51 36 36 0 0 1 69-6 30 30 0 0 1 16 57z"
          fill="url(#es-b)"
          stroke="url(#es-a)"
          stroke-width="2.5"
          stroke-linejoin="round"
        />
        <path d="M96 86l14-16 14 16M110 70v30" fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" />
      </g>
      <g class="spark s1"><path d="M30 40l3 8 8 3-8 3-3 8-3-8-8-3 8-3z" fill="#f472b6" /></g>
      <g class="spark s2"><path d="M188 30l2.5 6 6 2.5-6 2.5-2.5 6-2.5-6-6-2.5 6-2.5z" fill="#22d3ee" /></g>
      <g class="spark s3"><circle cx="176" cy="96" r="3.5" fill="#fbbf24" /></g>
    </svg>
    <h3>{{ title }}</h3>
    <p v-if="hint">{{ hint }}</p>
    <div v-if="$slots.default" class="actions"><slot /></div>
  </div>
</template>

<style scoped>
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 28px 16px;
}

.art {
  width: 200px;
  max-width: 60%;
  filter: drop-shadow(0 14px 30px rgba(99, 102, 241, 0.4));
}

.cloud {
  animation: bob 4.5s ease-in-out infinite;
  transform-origin: center;
}

.spark {
  animation: twinkle 3s ease-in-out infinite;
  transform-box: fill-box;
  transform-origin: center;
}

.s2 { animation-delay: 0.8s; }
.s3 { animation-delay: 1.6s; }

h3 {
  margin-top: 6px;
  font-size: 18px;
}

p {
  margin: 6px 0 0;
  color: var(--text-dim);
  max-width: 34ch;
}

.actions {
  margin-top: 16px;
}

@keyframes bob {
  50% {
    transform: translateY(-7px);
  }
}

@keyframes twinkle {
  50% {
    transform: scale(1.5) rotate(20deg);
    opacity: 0.45;
  }
}
</style>
