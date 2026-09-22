<script setup lang="ts">
// Переход по ссылке из письма подтверждения (email.setIntro) — доступен без входа: письмо могли открыть
// в другом браузере или на телефоне. Подтверждает сразу при открытии страницы.
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import { ApiError } from '@/api/client'
import { authApi } from '@/api/endpoints'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const auth = useAuthStore()

const status = ref<'checking' | 'done' | 'failed'>('checking')
const error = ref('')

onMounted(async () => {
  const token = typeof route.query.token === 'string' ? route.query.token : ''
  if (!token) {
    status.value = 'failed'
    error.value = t('emailConfirm.noToken')
    return
  }
  try {
    await authApi.confirmEmail({ token })
    status.value = 'done'
    if (auth.isAuthenticated) await auth.load(true) // тот же браузер, где почту и указывали
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    status.value = 'failed'
    error.value = describeApiError(err)
  }
})
</script>

<template>
  <UiSheet as="article">
    <UiPageHeader :title="$t('emailConfirm.title')" />
    <p v-if="status === 'checking'" data-testid="email-confirm-checking">{{ $t('emailConfirm.checking') }}</p>
    <template v-else-if="status === 'done'">
      <UiAlert tone="success" data-testid="email-confirm-done">{{ $t('emailConfirm.done') }}</UiAlert>
      <p><UiButton variant="primary" :to="auth.isAuthenticated ? '/file' : '/'">{{ $t('emailConfirm.back') }}</UiButton></p>
    </template>
    <template v-else>
      <UiAlert tone="danger" data-testid="email-confirm-failed">{{ error }}</UiAlert>
      <p><UiButton :to="auth.isAuthenticated ? '/file' : '/'">{{ $t('emailConfirm.back') }}</UiButton></p>
    </template>
  </UiSheet>
</template>
