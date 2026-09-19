<script setup lang="ts">
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'
</script>

<template>
  <UiNotice stamp="Изъято" title="Дело не найдено">
    <p>Запрошенный документ отсутствует в архиве или изъят. Проверьте адрес.</p>
    <template #actions>
      <UiButton to="/" variant="primary" icon="home">На главную</UiButton>
      <UiButton to="/catalog" icon="book">В каталог</UiButton>
    </template>
  </UiNotice>
</template>
