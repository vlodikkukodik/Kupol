<script setup lang="ts">
import UiSheet from './UiSheet.vue'
import UiStamp from './UiStamp.vue'

// Страница-уведомление: лист с крупным штампом, заголовком и пояснением («Дело не найдено», «Сбой архива», «Доступ запрещён»).
withDefaults(defineProps<{ stamp: string; title: string; tone?: 'red' | 'ink'; role?: 'alert' | 'status' }>(), { tone: 'red', role: undefined })
</script>

<template>
  <UiSheet as="article" class="ui-notice" :role="role" fold>
    <UiStamp :text="stamp" :tone="tone" size="lg" class="ui-notice__stamp" />
    <h1>{{ title }}</h1>
    <div class="ui-notice__body"><slot /></div>
    <div v-if="$slots.actions" class="ui-notice__actions"><slot name="actions" /></div>
  </UiSheet>
</template>

<style scoped>
.ui-notice {
  margin-top: var(--space-6);
}
.ui-notice__stamp {
  margin-bottom: var(--space-4);
}
.ui-notice h1 {
  font-size: var(--text-3xl);
}
.ui-notice__body {
  max-width: 40rem;
}
.ui-notice__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-top: var(--space-4);
}
</style>
