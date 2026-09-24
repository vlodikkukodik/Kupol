<template>
  <div class="aurora" aria-hidden="true">
    <span class="blob b1" />
    <span class="blob b2" />
    <span class="blob b3" />
    <span class="grid" />
  </div>
</template>

<style scoped>
.aurora {
  position: fixed;
  inset: 0;
  z-index: -1;
  overflow: hidden;
  background: radial-gradient(120% 90% at 50% -10%, #12163a 0%, var(--bg-0) 60%);
}

.blob {
  position: absolute;
  border-radius: 50%;
  /* Мягкий край даёт сам радиальный градиент; filter: blur здесь не нужен и сильно нагружает видеокарту. */
  opacity: 0.6;
  will-change: transform;
}

.b1 {
  width: 46vmax;
  height: 46vmax;
  left: -12vmax;
  top: -14vmax;
  background: radial-gradient(circle closest-side, #6366f1, transparent);
  animation: float1 26s ease-in-out infinite alternate;
}

.b2 {
  width: 40vmax;
  height: 40vmax;
  right: -12vmax;
  top: 6vmax;
  background: radial-gradient(circle closest-side, #ec4899, transparent);
  opacity: 0.45;
  animation: float2 32s ease-in-out infinite alternate;
}

.b3 {
  width: 44vmax;
  height: 44vmax;
  left: 24vmax;
  bottom: -22vmax;
  background: radial-gradient(circle closest-side, #06b6d4, transparent);
  opacity: 0.38;
  animation: float3 38s ease-in-out infinite alternate;
}

/* Едва заметная сетка добавляет глубины */
.grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.035) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.035) 1px, transparent 1px);
  background-size: 56px 56px;
  mask-image: radial-gradient(70% 60% at 50% 30%, #000 0%, transparent 100%);
}

@keyframes float1 {
  to {
    transform: translate(9vmax, 7vmax) scale(1.15);
  }
}

@keyframes float2 {
  to {
    transform: translate(-8vmax, 9vmax) scale(0.9);
  }
}

@keyframes float3 {
  to {
    transform: translate(-10vmax, -6vmax) scale(1.2);
  }
}
</style>
