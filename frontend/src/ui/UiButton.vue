<script setup lang="ts">
import { computed } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import UiIcon from './UiIcon.vue'
import type { IconName } from './icons'

// Кнопка и ссылка-кнопка. С `to` рисуется RouterLink, с `href` — обычная ссылка, иначе — <button>.
const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'link'
    size?: 'sm' | 'md' | 'lg'
    icon?: IconName
    iconEnd?: IconName
    to?: RouteLocationRaw
    href?: string
    type?: 'button' | 'submit' | 'reset'
    block?: boolean
    loading?: boolean
    disabled?: boolean
  }>(),
  { variant: 'secondary', size: 'md', type: 'button', block: false, loading: false, disabled: false },
)

const classes = computed(() => ['ui-button', `ui-button--${props.variant}`, `ui-button--${props.size}`, { 'ui-button--block': props.block, 'is-loading': props.loading }])
const inactive = computed(() => props.disabled || props.loading)
</script>

<template>
  <RouterLink v-if="to && !inactive" :class="classes" :to="to">
    <UiIcon v-if="icon" :name="icon" />
    <span class="ui-button__label"><slot /></span>
    <UiIcon v-if="iconEnd" :name="iconEnd" />
  </RouterLink>
  <a v-else-if="href && !inactive" :class="classes" :href="href">
    <UiIcon v-if="icon" :name="icon" />
    <span class="ui-button__label"><slot /></span>
    <UiIcon v-if="iconEnd" :name="iconEnd" />
  </a>
  <button v-else :class="classes" :type="type" :disabled="inactive" :aria-busy="loading ? 'true' : undefined">
    <UiIcon v-if="icon" :name="icon" />
    <span class="ui-button__label"><slot /></span>
    <UiIcon v-if="iconEnd" :name="iconEnd" />
  </button>
</template>

<style scoped>
.ui-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: var(--control-h);
  padding: 0 var(--space-5);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: transparent;
  color: var(--ink-900);
  font-family: var(--font-head);
  font-size: var(--text-md);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  line-height: 1.15;
  text-align: center;
  text-decoration: none;
  text-transform: uppercase;
  cursor: pointer;
  transition:
    background var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease),
    transform var(--dur-fast) var(--ease);
}
.ui-button:hover {
  background: var(--ink-900);
  color: var(--paper-50);
}
.ui-button:active {
  transform: translateY(1px);
}
.ui-button--sm {
  min-height: var(--control-h-sm);
  padding: 0 var(--space-4);
  font-size: var(--text-sm);
}
.ui-button--lg {
  min-height: 3.25rem;
  padding: 0 var(--space-6);
  font-size: var(--text-lg);
}
.ui-button--block {
  display: flex;
  width: 100%;
}

.ui-button--primary {
  background: var(--ink-900);
  color: var(--paper-50);
}
.ui-button--primary:hover {
  background: var(--red-700);
  border-color: var(--red-700);
}

.ui-button--danger {
  border-color: var(--red-700);
  color: var(--red-700);
}
.ui-button--danger:hover {
  background: var(--red-700);
  color: var(--paper-50);
}

.ui-button--ghost {
  border-color: transparent;
}
.ui-button--ghost:hover {
  background: rgb(0 0 0 / 0.08);
  color: inherit;
}

/* «Ссылка-кнопка» без рамки: для второстепенных действий внутри форм и таблиц */
.ui-button--link {
  min-height: 2rem;
  padding: 0 var(--space-1);
  border: 0;
  color: var(--link);
  font-family: var(--font-ui);
  font-size: inherit;
  font-weight: 400;
  letter-spacing: 0;
  text-decoration: underline;
  text-decoration-thickness: 0.08em;
  text-underline-offset: 0.18em;
  text-transform: none;
}
.ui-button--link:hover {
  background: transparent;
  color: var(--link-hover);
}

.ui-button:disabled,
.ui-button.is-loading {
  opacity: 0.55;
  cursor: not-allowed;
}
.ui-button.is-loading {
  cursor: progress;
}

/* Кнопка на тёмном столе (в шапке и на панелях цвета стола) */
:global(.on-desk) .ui-button {
  border-color: var(--on-bg);
  color: var(--on-bg);
}
:global(.on-desk) .ui-button:hover {
  background: var(--on-bg);
  color: var(--desk-900);
}
:global(.on-desk) .ui-button--primary {
  background: var(--paper-50);
  color: var(--ink-900);
}
:global(.on-desk) .ui-button--primary:hover {
  background: var(--amber-300);
  border-color: var(--amber-300);
  color: var(--ink-950);
}
</style>
