<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'

defineProps<{ requestId?: string; retrying?: boolean }>()
defineEmits<{ retry: [] }>()
</script>

<template>
  <UiNotice stamp="Сбой" title="Сбой архива" role="alert">
    <p>Архив временно недоступен. Повторите попытку позже.</p>
    <p v-if="requestId" class="ref">Номер обращения: <code>{{ requestId }}</code></p>
    <template #actions>
      <UiButton variant="primary" :loading="retrying" icon="refresh" @click="$emit('retry')">{{ retrying ? 'Проверка…' : 'Повторить' }}</UiButton>
    </template>
  </UiNotice>
</template>

<style scoped>
.ref {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
