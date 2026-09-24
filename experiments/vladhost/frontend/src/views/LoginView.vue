<script setup lang="ts">
import { LockClosedOutline, PersonOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput } from 'naive-ui'
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { fieldErrors, loginForm } from '@/api/schemas'
import AuthShell from '@/components/AuthShell.vue'
import { resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({ login: '', password: '' })
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

async function submit() {
  failure.value = ''
  const parsed = loginForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await auth.login(parsed.data.login, parsed.data.password)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.push(next)
  } catch (e) {
    failure.value = e instanceof ApiError ? e.message : t('auth.login.failed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <auth-shell>
    <h2 class="title">{{ t('auth.login.title') }}</h2>
    <p class="subtitle">{{ t('auth.login.subtitle') }}</p>

    <n-form @submit.prevent="submit">
      <n-form-item
        :label="t('auth.login.identity')"
        :validation-status="errors.login ? 'error' : undefined"
        :feedback="errors.login ? resolveMessage(errors.login) : undefined"
      >
        <n-input
          v-model:value="form.login"
          size="large"
          placeholder="name@example.com"
          autofocus
          autocomplete="username"
          :input-props="{ 'aria-label': t('auth.login.identity') }"
        >
          <template #prefix><n-icon :component="PersonOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item
        :label="t('auth.login.password')"
        :validation-status="errors.password ? 'error' : undefined"
        :feedback="errors.password ? resolveMessage(errors.password) : undefined"
      >
        <n-input
          v-model:value="form.password"
          size="large"
          placeholder="••••••••"
          type="password"
          show-password-on="click"
          autocomplete="current-password"
          :input-props="{ 'aria-label': t('auth.login.password') }"
        >
          <template #prefix><n-icon :component="LockClosedOutline" /></template>
        </n-input>
      </n-form-item>

      <transition name="page">
        <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
      </transition>

      <n-button type="primary" size="large" attr-type="submit" block :loading="busy">{{ t('auth.login.submit') }}</n-button>
    </n-form>

    <p class="hint">
      {{ t('auth.login.inviteOnly') }}
      <router-link to="/register">{{ t('auth.login.haveCode') }}</router-link>
    </p>
  </auth-shell>
</template>

<style scoped>
.title {
  font-size: 26px;
  font-weight: 750;
}

.subtitle {
  margin: 6px 0 22px;
  color: var(--text-dim);
}

.gap {
  margin-bottom: 14px;
}

.hint {
  margin: 20px 0 0;
  font-size: 14px;
  color: var(--text-dim);
}
</style>
