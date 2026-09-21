<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'
</script>

<template>
  <UiNotice :stamp="$t('notice.notFound.stamp')" :title="$t('notice.notFound.title')">
    <p>{{ $t('notice.notFound.text') }}</p>
    <template #actions>
      <UiButton to="/" variant="primary" icon="home">{{ $t('notice.notFound.home') }}</UiButton>
      <UiButton to="/catalog" icon="book">{{ $t('notice.notFound.catalog') }}</UiButton>
    </template>
  </UiNotice>
</template>
