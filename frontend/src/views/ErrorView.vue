<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'

defineProps<{ requestId?: string; retrying?: boolean }>()
defineEmits<{ retry: [] }>()
</script>

<template>
  <UiNotice :stamp="$t('notice.failure.stamp')" :title="$t('notice.failure.title')" role="alert">
    <p>{{ $t('notice.failure.text') }}</p>
    <p v-if="requestId" class="ref">{{ $t('notice.failure.ref') }} <code>{{ requestId }}</code></p>
    <template #actions>
      <UiButton variant="primary" :loading="retrying" icon="refresh" @click="$emit('retry')">{{ retrying ? $t('notice.checking') : $t('notice.failure.retry') }}</UiButton>
    </template>
  </UiNotice>
</template>

<style scoped>
.ref {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
