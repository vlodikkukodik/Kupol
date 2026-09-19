<script setup lang="ts">
// Штамп-оттиск: рамка и надпись красной или чёрной краской с неровным краем (маска ink.svg), под наклоном.
// Текст в штампе — настоящий, поэтому читается скринридером; анимация «падения» отключается при prefers-reduced-motion.
withDefaults(
  defineProps<{
    text: string
    tone?: 'red' | 'ink'
    /** Наклон, градусы */
    tilt?: number
    animate?: boolean
    size?: 'sm' | 'md' | 'lg'
  }>(),
  { tone: 'red', tilt: -6, animate: true, size: 'md' },
)
</script>

<template>
  <span class="ui-stamp" :class="[`ui-stamp--${tone}`, `ui-stamp--${size}`, { 'ui-stamp--animate': animate }]" :style="{ '--tilt': `${tilt}deg` }">
    {{ text }}
  </span>
</template>

<style scoped>
.ui-stamp {
  display: inline-block;
  padding: 0.12em 0.55em;
  border: 0.14em solid currentColor;
  border-radius: 0.2em;
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: 0.14em;
  line-height: 1.2;
  text-transform: uppercase;
  transform: rotate(var(--tilt));
  mix-blend-mode: multiply;
  opacity: 0.9;
  user-select: none;
  -webkit-mask: var(--texture-ink);
  mask: var(--texture-ink);
  -webkit-mask-size: 260px 260px;
  mask-size: 260px 260px;
}
.ui-stamp--sm { font-size: var(--text-md); }
.ui-stamp--md { font-size: var(--text-xl); }
.ui-stamp--lg { font-size: var(--text-3xl); }
.ui-stamp--red { color: var(--red-700); }
.ui-stamp--ink { color: var(--ink-900); }

.ui-stamp--animate {
  animation: ui-stamp-drop 0.42s cubic-bezier(0.2, 0.9, 0.3, 1.2) both;
}
@keyframes ui-stamp-drop {
  from {
    opacity: 0;
    transform: rotate(var(--tilt)) scale(1.9);
  }
  60% {
    opacity: 0.95;
  }
  to {
    opacity: 0.9;
    transform: rotate(var(--tilt)) scale(1);
  }
}
</style>
