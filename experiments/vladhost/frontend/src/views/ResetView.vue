<script setup lang="ts">
import { LockClosedOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput } from 'naive-ui'
import { computed, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { fieldErrors, resetForm } from '@/api/schemas'
import AuthShell from '@/components/AuthShell.vue'
import { resolveMessage, useI18n } from '@/i18n'

// Новый пароль по ссылке из письма (?token=…). Ссылка одноразовая и живёт час; после смены все сессии аккаунта закрываются.
const { t } = useI18n()
const route = useRoute()
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''))

const form = reactive({ password: '', repeat: '' })
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)
const done = ref(false)

async function submit() {
  failure.value = ''
  const parsed = resetForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await api('/api/auth/reset', { method: 'POST', body: { token: token.value, password: parsed.data.password }, auth: false })
    done.value = true
  } catch (e) {
    if (e instanceof ApiError && e.field === 'password') errors.value = { password: e.message }
    else failure.value = e instanceof ApiError ? e.message : t('auth.reset.failed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <auth-shell>
    <h2 class="title">{{ t('auth.reset.title') }}</h2>

    <template v-if="!token">
      <n-alert type="error" :show-icon="false" class="gap">{{ t('auth.reset.noToken') }}</n-alert>
      <p class="hint"><router-link to="/forgot">{{ t('auth.reset.askAgain') }}</router-link></p>
    </template>

    <template v-else-if="done">
      <n-alert type="success" :show-icon="false" class="gap">{{ t('auth.reset.done') }}</n-alert>
      <n-button type="primary" size="large" block @click="$router.push('/login')">{{ t('auth.reset.toLogin') }}</n-button>
    </template>

    <template v-else>
      <p class="subtitle">{{ t('auth.reset.subtitle') }}</p>
      <n-form @submit.prevent="submit">
        <n-form-item
          :label="t('auth.reset.password')"
          :validation-status="errors.password ? 'error' : undefined"
          :feedback="errors.password ? resolveMessage(errors.password) : undefined"
        >
          <n-input
            v-model:value="form.password"
            size="large"
            type="password"
            show-password-on="click"
            autofocus
            autocomplete="new-password"
            :input-props="{ 'aria-label': t('auth.reset.password') }"
          >
            <template #prefix><n-icon :component="LockClosedOutline" /></template>
          </n-input>
        </n-form-item>
        <n-form-item
          :label="t('auth.reset.repeat')"
          :validation-status="errors.repeat ? 'error' : undefined"
          :feedback="errors.repeat ? resolveMessage(errors.repeat) : undefined"
        >
          <n-input
            v-model:value="form.repeat"
            size="large"
            type="password"
            show-password-on="click"
            autocomplete="new-password"
            :input-props="{ 'aria-label': t('auth.reset.repeat') }"
          >
            <template #prefix><n-icon :component="LockClosedOutline" /></template>
          </n-input>
        </n-form-item>

        <transition name="page">
          <n-alert v-if="failure" type="error" :show-icon="false" class="gap">
            {{ failure }} <router-link to="/forgot">{{ t('auth.reset.askAgain') }}</router-link>
          </n-alert>
        </transition>

        <n-button type="primary" size="large" attr-type="submit" block :loading="busy">{{ t('auth.reset.submit') }}</n-button>
      </n-form>
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
