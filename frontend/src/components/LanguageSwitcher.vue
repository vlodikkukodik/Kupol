<script setup lang="ts">
import { LOCALES, locale, setLocale } from '@/i18n'

// Переключатель языка интерфейса: RU | IT. Название языка на самом языке (lang у кнопки) — скринридер произнесёт его правильно.
</script>

<template>
  <div class="lang" role="group" :aria-label="$t('lang.switchTo')" data-testid="lang-switcher">
    <button
      v-for="code in LOCALES"
      :key="code"
      type="button"
      class="lang__btn"
      :lang="code"
      :aria-pressed="locale === code ? 'true' : 'false'"
      :aria-label="`${$t(`lang.short.${code}`)} — ${$t(`lang.native.${code}`)}`"
      :title="$t(`lang.native.${code}`)"
      :data-testid="`lang-${code}`"
      @click="setLocale(code)"
    >
      {{ $t(`lang.short.${code}`) }}
    </button>
  </div>
</template>

<style scoped>
.lang {
  display: inline-flex;
  border: 1px solid var(--ink-900);
}
.lang__btn {
  min-width: 2.75rem;
  min-height: 2.75rem;
  padding: 0 var(--space-2);
  border: 0;
  background: transparent;
  color: var(--text);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  cursor: pointer;
}
.lang__btn + .lang__btn {
  border-left: 1px solid var(--ink-900);
}
.lang__btn:hover {
  background: var(--surface-strong);
}
.lang__btn[aria-pressed='true'] {
  background: var(--ink-900);
  color: var(--paper-50);
}
</style>
