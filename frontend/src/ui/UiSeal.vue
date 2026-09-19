<script setup lang="ts">
// Печать КУПОЛ: круг, текст по окружности, купол и год основания.
withDefaults(defineProps<{ size?: number | string; decorative?: boolean }>(), { size: 120, decorative: false })
</script>

<template>
  <svg
    class="ui-seal"
    viewBox="0 0 200 200"
    :width="size"
    :height="size"
    :role="decorative ? undefined : 'img'"
    :aria-label="decorative ? undefined : 'Печать КУПОЛ'"
    :aria-hidden="decorative ? 'true' : undefined"
    focusable="false"
  >
    <defs>
      <path id="ui-seal-ring" d="M 100,100 m -76,0 a 76,76 0 1,1 152,0 a 76,76 0 1,1 -152,0" />
    </defs>
    <circle cx="100" cy="100" r="96" class="line" fill="none" stroke-width="3" />
    <circle cx="100" cy="100" r="90" class="line" fill="none" stroke-width="1" />
    <circle cx="100" cy="100" r="58" class="line" fill="none" stroke-width="2" />
    <text class="ring-text" font-size="13">
      <textPath href="#ui-seal-ring" startOffset="0" textLength="470" lengthAdjust="spacing">
        КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ
      </textPath>
    </text>
    <path d="M 62,112 A 38,38 0 0 1 138,112 Z" class="fill" />
    <line x1="56" y1="112" x2="144" y2="112" class="line" stroke-width="3" />
    <line x1="100" y1="74" x2="100" y2="62" class="line" stroke-width="3" />
    <circle cx="100" cy="60" r="3.5" class="fill" />
    <text x="100" y="140" text-anchor="middle" class="year" font-size="20">1974</text>
  </svg>
</template>

<style scoped>
.ui-seal {
  color: var(--red-700);
}
.line {
  stroke: currentColor;
}
.fill {
  fill: currentColor;
}
.ring-text,
.year {
  fill: currentColor;
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: 0.08em;
}
</style>
