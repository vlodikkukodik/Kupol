<script setup lang="ts">
import { GlobeOutline, KeyOutline, LockClosedOutline, MailOutline, PersonOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput } from 'naive-ui'
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { fieldErrors, registerForm } from '@/api/schemas'
import AuthShell from '@/components/AuthShell.vue'
import { resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({
  invite: typeof route.query.invite === 'string' ? route.query.invite : '',
  email: '',
  username: '',
  password: '',
})
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

const hostPreview = computed(() => `*.${form.username.trim().toLowerCase() || '…'}.vladinc.ru`)

async function submit() {
  failure.value = ''
  const parsed = registerForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await auth.register(parsed.data)
    await router.push('/')
  } catch (e) {
    if (e instanceof ApiError && e.field) {
      errors.value = { [e.field]: e.message }
    } else {
      failure.value = e instanceof ApiError ? e.message : t('auth.register.failed')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <auth-shell>
    <h2 class="title">{{ t('auth.register.title') }}</h2>
    <p class="subtitle">{{ t('auth.register.subtitle') }}</p>

    <n-form @submit.prevent="submit">
      <n-form-item
        :label="t('auth.register.invite')"
        :validation-status="errors.invite ? 'error' : undefined"
        :feedback="errors.invite ? resolveMessage(errors.invite) : undefined"
      >
        <n-input v-model:value="form.invite" size="large" placeholder="XXXXXXXXXXXXXXXX" autocomplete="off" :input-props="{ 'aria-label': t('auth.register.invite') }">
          <template #prefix><n-icon :component="KeyOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item
        :label="t('auth.register.email')"
        :validation-status="errors.email ? 'error' : undefined"
        :feedback="errors.email ? resolveMessage(errors.email) : undefined"
      >
        <n-input v-model:value="form.email" size="large" placeholder="name@example.com" autocomplete="email" :input-props="{ 'aria-label': t('auth.register.email') }">
          <template #prefix><n-icon :component="MailOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item
        :label="t('auth.register.username')"
        :validation-status="errors.username ? 'error' : undefined"
        :feedback="errors.username ? resolveMessage(errors.username) : t('auth.register.addresses', { host: hostPreview })"
      >
        <n-input
          v-model:value="form.username"
          size="large"
          placeholder="john"
          autocomplete="username"
          :input-props="{ 'aria-label': t('auth.register.username') }"
        >
          <template #prefix><n-icon :component="errors.username ? PersonOutline : GlobeOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item
        :label="t('auth.register.password')"
        :validation-status="errors.password ? 'error' : undefined"
        :feedback="errors.password ? resolveMessage(errors.password) : undefined"
      >
        <n-input
          v-model:value="form.password"
          size="large"
          placeholder="••••••••"
          type="password"
          show-password-on="click"
          autocomplete="new-password"
          :input-props="{ 'aria-label': t('auth.register.password') }"
        >
          <template #prefix><n-icon :component="LockClosedOutline" /></template>
        </n-input>
      </n-form-item>

      <transition name="page">
        <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
      </transition>

      <n-button type="primary" size="large" attr-type="submit" block :loading="busy">{{ t('auth.register.submit') }}</n-button>
    </n-form>

    <p class="hint"><router-link to="/login">{{ t('auth.register.haveAccount') }}</router-link></p>
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
}
</style>
