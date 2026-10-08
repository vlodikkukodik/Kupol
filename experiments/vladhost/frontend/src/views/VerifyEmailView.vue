<script setup lang="ts">
import { NAlert, NButton, NSpin } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError } from '@/api/client'
import AuthShell from '@/components/AuthShell.vue'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

// Подтверждение адреса по ссылке из письма: открывается и без входа в панель, и во вкладке, где вход уже выполнен.
const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''))
const state = ref<'working' | 'ok' | 'error'>('working')
const message = ref('')

onMounted(async () => {
  if (!token.value) {
    state.value = 'error'
    message.value = t('auth.verify.noToken')
    return
  }
  try {
    await api('/api/auth/verify-email', { method: 'POST', body: { token: token.value }, auth: false })
    state.value = 'ok'
    if (auth.user) await auth.refreshMe().catch(() => {}) // баннер «подтвердите почту» исчезает сразу
  } catch (e) {
    state.value = 'error'
    message.value = e instanceof ApiError ? e.message : t('auth.verify.failed')
  }
})
</script>

<template>
  <auth-shell>
    <h2 class="title">{{ t('auth.verify.title') }}</h2>
    <div v-if="state === 'working'" class="wait"><n-spin size="small" /> {{ t('auth.verify.working') }}</div>
    <template v-else-if="state === 'ok'">
      <n-alert type="success" :show-icon="false" class="gap">{{ t('auth.verify.done') }}</n-alert>
      <n-button type="primary" size="large" block @click="$router.push(auth.user ? '/' : '/login')">
        {{ auth.user ? t('auth.verify.toPanel') : t('auth.reset.toLogin') }}
      </n-button>
    </template>
    <template v-else>
      <n-alert type="error" :show-icon="false" class="gap">{{ message }}</n-alert>
      <p class="hint">{{ t('auth.verify.hint') }}</p>
    </template>
  </auth-shell>
</template>

<style scoped>
.title {
  font-size: 26px;
  font-weight: 750;
  margin-bottom: 18px;
}

.wait {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--text-dim);
}

.gap {
  margin-bottom: 16px;
}

.hint {
  font-size: 14px;
  color: var(--text-dim);
}
</style>
