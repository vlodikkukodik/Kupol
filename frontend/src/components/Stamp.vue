<script setup>
defineProps({
  /** Текст штампа, например «ИЗЪЯТО» */
  text: { type: String, required: true },
  /** red — красная печать, ink — чёрная */
  tone: { type: String, default: 'red', validator: (v) => ['red', 'ink'].includes(v) },
  /** Наклон, градусы */
  tilt: { type: Number, default: -6 },
  /** Анимация «штамп падает» при появлении */
  animate: { type: Boolean, default: true },
})
</script>

<template>
  <span class="stamp" :class="[`stamp--${tone}`, { 'stamp--animate': animate }]" :style="{ '--tilt': `${tilt}deg` }">
    {{ text }}
  </span>
</template>

<style scoped>
.stamp {
  display: inline-block;
  padding: 0.15em 0.6em;
  border: 0.14em solid currentColor;
  border-radius: 0.2em;
  font-family: var(--font-head);
  font-weight: 700;
  font-size: 1.5rem;
  letter-spacing: 0.14em;
  line-height: 1.2;
  text-transform: uppercase;
  transform: rotate(var(--tilt));
  mix-blend-mode: multiply;
  opacity: 0.92;
  user-select: none;
  /* «выцветание» краски: тонкая внутренняя рамка */
  box-shadow: inset 0 0 0 0.09em rgb(233 224 200 / 0.55);
}
.stamp--red { color: var(--stamp-red); }
.stamp--ink { color: var(--ink); }

.stamp--animate { animation: stamp-drop 0.42s cubic-bezier(0.2, 0.9, 0.3, 1.2) both; }

@keyframes stamp-drop {
  from { opacity: 0; transform: rotate(var(--tilt)) scale(1.9); }
  60% { opacity: 0.95; }
  to { opacity: 0.92; transform: rotate(var(--tilt)) scale(1); }
}
</style>
