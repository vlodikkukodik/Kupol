<script setup lang="ts">
import { darkTheme, dateRuRU, NConfigProvider, NDialogProvider, NMessageProvider, ruRU, type GlobalThemeOverrides } from 'naive-ui'

const overrides: GlobalThemeOverrides = {
  common: { primaryColor: '#4ade80', primaryColorHover: '#6ee79a', primaryColorPressed: '#38c46c', primaryColorSuppl: '#4ade80' },
}
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides" :locale="ruRU" :date-locale="dateRuRU" abstract>
    <n-dialog-provider>
      <n-message-provider>
        <router-view />
      </n-message-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>
