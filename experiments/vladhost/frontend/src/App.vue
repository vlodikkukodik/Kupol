<script setup lang="ts">
import {
  darkTheme,
  dateItIT,
  dateRuRU,
  itIT,
  NConfigProvider,
  NDialogProvider,
  NMessageProvider,
  ruRU,
  type GlobalThemeOverrides,
} from 'naive-ui'
import { computed } from 'vue'
import AuroraBackground from '@/components/AuroraBackground.vue'
import { useI18n } from '@/i18n'

const { locale } = useI18n()

// Локаль компонентов Naive UI (подписи в календарях, «Подтвердить/Отмена» и т. п.) следует за языком интерфейса.
const naiveLocale = computed(() => (locale.value === 'it' ? itIT : ruRU))
const naiveDateLocale = computed(() => (locale.value === 'it' ? dateItIT : dateRuRU))

const overrides: GlobalThemeOverrides = {
  common: {
    fontFamily: "'Inter Variable', ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
    primaryColor: '#8b5cf6',
    primaryColorHover: '#a78bfa',
    primaryColorPressed: '#7c3aed',
    primaryColorSuppl: '#8b5cf6',
    successColor: '#34d399',
    successColorHover: '#6ee7b7',
    warningColor: '#fbbf24',
    errorColor: '#fb7185',
    errorColorHover: '#fda4af',
    infoColor: '#22d3ee',
    borderRadius: '12px',
    bodyColor: 'transparent',
    cardColor: 'rgba(255, 255, 255, 0.05)',
    modalColor: '#141733',
    popoverColor: '#171a35',
    tableColor: 'transparent',
    inputColor: 'rgba(255, 255, 255, 0.05)',
    textColorBase: '#f1f2ff',
    textColor1: '#f1f2ff',
    textColor2: 'rgba(241, 242, 255, 0.84)',
    textColor3: 'rgba(241, 242, 255, 0.56)',
    dividerColor: 'rgba(255, 255, 255, 0.09)',
    borderColor: 'rgba(255, 255, 255, 0.12)',
  },
  Button: {
    borderRadiusTiny: '9px',
    borderRadiusSmall: '10px',
    borderRadiusMedium: '12px',
    borderRadiusLarge: '14px',
    heightMedium: '40px',
    heightLarge: '48px',
    fontWeight: '600',
  },
  Input: {
    color: 'rgba(255, 255, 255, 0.05)',
    colorFocus: 'rgba(255, 255, 255, 0.075)',
    border: '1px solid rgba(255, 255, 255, 0.13)',
    borderHover: '1px solid rgba(167, 139, 250, 0.65)',
    borderFocus: '1px solid rgba(167, 139, 250, 0.95)',
    boxShadowFocus: 'none',
    borderRadius: '12px',
    heightMedium: '42px',
    heightLarge: '50px',
  },
  Card: { borderRadius: '18px' },
  Message: { borderRadius: '14px' },
  Popover: { borderRadius: '14px' },
  Dropdown: { borderRadius: '14px', optionColorHover: 'rgba(167, 139, 250, 0.18)' },
}
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale"
    :date-locale="naiveDateLocale"
    abstract
  >
    <n-dialog-provider>
      <n-message-provider placement="top">
        <aurora-background />
        <router-view />
      </n-message-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>
