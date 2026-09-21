<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'

defineProps<{ retrying?: boolean }>()
defineEmits<{ retry: [] }>()
</script>

<template>
  <UiNotice :stamp="$t('notice.closed.stamp')" tone="ink" :title="$t('notice.closed.title')" role="status">
    <p>{{ $t('notice.closed.text') }}</p>
    <template #actions>
      <UiButton variant="primary" :loading="retrying" icon="refresh" @click="$emit('retry')">{{ retrying ? $t('notice.checking') : $t('notice.closed.retry') }}</UiButton>
    </template>
  </UiNotice>
</template>
