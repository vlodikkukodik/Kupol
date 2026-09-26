<script setup lang="ts">
import { KeyOutline, LockClosedOutline, PersonOutline } from '@vicons/ionicons5'
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
// Второй шаг: пароль принят, ждём код из приложения (или код восстановления).
const ticket = ref('')
const code = ref('')

async function done() {
  const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
  await router.push(next)
}

async function submit() {
  failure.value = ''
  const parsed = loginForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    const challenge = await auth.login(parsed.data.login, parsed.data.password)
    if (challenge) {
      ticket.value = challenge.ticket
      code.value = ''
      return
    }
    await done()
  } catch (e) {
    failure.value = e instanceof ApiError ? e.message : t('auth.login.failed')
  } finally {
    busy.value = false
  }
}

async function submitCode() {
  failure.value = ''
  if (!code.value.trim()) {
    failure.value = t('auth.login.twoFactor.required')
    return
  }
  busy.value = true
  try {
    await auth.loginSecondFactor(ticket.value, code.value)
    await done()
  } catch (e) {
    // Билет истёк или попытки кончились — снова с пароля.
    if (e instanceof ApiError && e.code === 'two_factor_ticket') restart()
    failure.value = e instanceof ApiError ? e.message : t('auth.login.failed')
  } finally {
    busy.value = false
  }
}

function restart() {
  ticket.value = ''
  code.value = ''
  form.password = ''
}
</script>

<template>
  <auth-shell>
    <template v-if="ticket">
      <h2 class="title">{{ t('auth.login.twoFactor.title') }}</h2>
      <p class="subtitle">{{ t('auth.login.twoFactor.subtitle') }}</p>
      <n-form @submit.prevent="submitCode">
        <n-form-item :label="t('auth.login.twoFactor.code')">
          <n-input
            v-model:value="code"
            size="large"
            placeholder="123 456"
            autofocus
            autocomplete="one-time-code"
            :maxlength="16"
            :input-props="{ 'aria-label': t('auth.login.twoFactor.code'), autocapitalize: 'off', spellcheck: false }"
          >
            <template #prefix><n-icon :component="KeyOutline" /></template>
          </n-input>
        </n-form-item>
        <transition name="page">
          <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
        </transition>
        <n-button type="primary" size="large" attr-type="submit" block :loading="busy">{{ t('auth.login.twoFactor.submit') }}</n-button>
      </n-form>
      <p class="hint">{{ t('auth.login.twoFactor.recoveryHint') }}</p>
      <p class="hint"><a href="#" @click.prevent="restart">{{ t('auth.login.twoFactor.back') }}</a></p>
    </template>

    <template v-else>
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

      <p class="hint"><router-link to="/forgot">{{ t('auth.login.forgot') }}</router-link></p>

      <p class="hint">
        {{ t('auth.login.inviteOnly') }}
        <router-link to="/register">{{ t('auth.login.haveCode') }}</router-link>
      </p>
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
  margin-bottom: 14px;
}

.hint {
  margin: 20px 0 0;
  font-size: 14px;
  color: var(--text-dim);
}
</style>
