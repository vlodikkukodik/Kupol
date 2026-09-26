<script setup lang="ts">
import { MailOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput } from 'naive-ui'
import { reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { fieldErrors, forgotForm } from '@/api/schemas'
import AuthShell from '@/components/AuthShell.vue'
import { resolveMessage, useI18n } from '@/i18n'

// Запрос ссылки для сброса пароля. Ответ сервера не зависит от того, есть ли такой адрес: страница тоже не раскрывает этого.
const { t } = useI18n()
const form = reactive({ email: '' })
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)
const sent = ref(false)

async function submit() {
  failure.value = ''
  const parsed = forgotForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await api('/api/auth/forgot', { method: 'POST', body: { email: parsed.data.email }, auth: false })
    sent.value = true
  } catch (e) {
    failure.value = e instanceof ApiError ? e.message : t('auth.forgot.failed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <auth-shell>
    <h2 class="title">{{ t('auth.forgot.title') }}</h2>

    <template v-if="sent">
      <n-alert type="success" :show-icon="false" class="gap">{{ t('auth.forgot.sent') }}</n-alert>
      <p class="hint"><router-link to="/login">{{ t('auth.forgot.backToLogin') }}</router-link></p>
    </template>

    <template v-else>
      <p class="subtitle">{{ t('auth.forgot.subtitle') }}</p>
      <n-form @submit.prevent="submit">
        <n-form-item
          :label="t('auth.forgot.email')"
          :validation-status="errors.email ? 'error' : undefined"
          :feedback="errors.email ? resolveMessage(errors.email) : undefined"
        >
          <n-input
            v-model:value="form.email"
            size="large"
            placeholder="name@example.com"
            autofocus
            autocomplete="email"
            :input-props="{ 'aria-label': t('auth.forgot.email'), type: 'email' }"
          >
            <template #prefix><n-icon :component="MailOutline" /></template>
          </n-input>
        </n-form-item>

        <transition name="page">
          <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
        </transition>

        <n-button type="primary" size="large" attr-type="submit" block :loading="busy">{{ t('auth.forgot.submit') }}</n-button>
      </n-form>
      <p class="hint"><router-link to="/login">{{ t('auth.forgot.backToLogin') }}</router-link></p>
    </template>
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
  margin: 14px 0;
}

.hint {
  margin: 20px 0 0;
  font-size: 14px;
  color: var(--text-dim);
}
</style>
