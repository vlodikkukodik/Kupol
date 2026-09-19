<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'

defineProps<{ retrying?: boolean }>()
defineEmits<{ retry: [] }>()
</script>

<template>
  <UiNotice stamp="Закрыто" tone="ink" title="Архив закрыт на инвентаризацию" role="status">
    <p>Доступ временно приостановлен. Загляните позже.</p>
    <template #actions>
      <UiButton variant="primary" :loading="retrying" icon="refresh" @click="$emit('retry')">{{ retrying ? 'Проверка…' : 'Проверить снова' }}</UiButton>
    </template>
  </UiNotice>
</template>
