<script setup lang="ts">
// Цветная «таблетка» статуса. Тон задаёт цвет; pulse — мягкая пульсация для процессов «в работе».
withDefaults(
  defineProps<{ tone?: 'emerald' | 'amber' | 'rose' | 'cyan' | 'violet' | 'slate'; pulse?: boolean }>(),
  { tone: 'slate', pulse: false },
)
</script>

<template>
  <span class="chip" :class="[`t-${tone}`, { pulse }]">
    <i class="dot" />
    <slot />
  </span>
</template>

<style scoped>
.chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 3px 11px 3px 9px;
  border-radius: 999px;
  font-size: 12.5px;
  font-weight: 650;
  white-space: nowrap;
  color: rgb(var(--c-text));
  background: rgb(var(--c) / 0.14);
  border: 1px solid rgb(var(--c) / 0.38);
  transition:
    background 0.3s,
    border-color 0.3s;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: rgb(var(--c));
  box-shadow: 0 0 10px rgb(var(--c) / 0.9);
}

.t-emerald { --c: 52 211 153; --c-text: 167 243 208; }
.t-amber { --c: 251 191 36; --c-text: 253 230 138; }
.t-rose { --c: 251 113 133; --c-text: 254 205 211; }
.t-cyan { --c: 34 211 238; --c-text: 165 243 252; }
.t-violet { --c: 167 139 250; --c-text: 221 214 254; }
.t-slate { --c: 148 163 184; --c-text: 203 213 225; }

.pulse .dot {
  animation: pulse 1.6s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    transform: scale(1);
    opacity: 1;
  }
  50% {
    transform: scale(1.7);
    opacity: 0.45;
  }
}
</style>
