<script setup lang="ts">
import { computed } from 'vue'
import { LOCALES, useI18n, type Locale } from '@/i18n'
import FlagIcon from './FlagIcon.vue'

// Переключатель языка (шапка, страницы входа, настройки). Выбор запоминается.
const { t, locale, setLocale } = useI18n()
const index = computed(() => LOCALES.indexOf(locale.value))

const names = { ru: 'lang.ru', it: 'lang.it' } as const
const pick = (l: Locale) => setLocale(l)
</script>

<template>
  <div class="switch" role="group" :aria-label="t('lang.label')" :style="{ '--i': index, '--n': LOCALES.length }">
    <span class="thumb" />
    <button
      v-for="l in LOCALES"
      :key="l"
      type="button"
      class="opt"
      :class="{ on: l === locale }"
      :aria-pressed="l === locale"
      :title="t(names[l])"
      :lang="l"
      @click="pick(l)"
    >
      <flag-icon class="flag" :locale="l" />
      <span class="code">{{ l.toUpperCase() }}</span>
    </button>
  </div>
</template>

<style scoped>
.switch {
  position: relative;
  display: inline-grid;
  grid-template-columns: repeat(var(--n), 1fr);
  padding: 4px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid var(--border);
}

.thumb {
  position: absolute;
  top: 4px;
  bottom: 4px;
  left: 4px;
  width: calc((100% - 8px) / var(--n));
  border-radius: 999px;
  background: var(--grad-primary);
  box-shadow: 0 6px 18px -4px rgba(139, 92, 246, 0.75);
  transform: translateX(calc(var(--i) * 100%));
  transition: transform 0.42s cubic-bezier(0.34, 1.4, 0.5, 1);
}

.opt {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  min-width: 68px;
  padding: 6px 12px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: var(--text-dim);
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: color 0.25s;
}

.opt:hover {
  color: #fff;
}

.opt.on {
  color: #fff;
}

.flag {
  transition: transform 0.3s var(--ease);
}

</style>
