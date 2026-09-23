<script setup lang="ts">
import { NCard, NDescriptions, NDescriptionsItem } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
</script>

<template>
  <div v-if="auth.user" class="page">
    <h2>Здравствуйте, {{ auth.user.username }}</h2>
    <n-card title="Ваш аккаунт" size="small">
      <n-descriptions :column="1" label-placement="left">
        <n-descriptions-item label="Email">{{ auth.user.email }}</n-descriptions-item>
        <n-descriptions-item label="Адреса сайтов">
          <code>{{ '{сайт}' }}.{{ auth.user.username }}.vladinc.ru</code>
        </n-descriptions-item>
        <n-descriptions-item label="Квоты">1 сайт, 500 МБ</n-descriptions-item>
      </n-descriptions>
    </n-card>
  </div>
</template>

<style scoped>
.page { max-width: 720px; }
h2 { margin: 0 0 16px; }
</style>
