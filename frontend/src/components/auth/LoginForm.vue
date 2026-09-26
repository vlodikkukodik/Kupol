<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import { useForm } from '@/composables/useForm'
import { t } from '@/i18n'
import { safeNextPath } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()
const form = useForm()
const login = ref('')
const password = ref('')
const totp = ref('')
// Второй шаг: пароль верен, а у человека включён код из приложения. Пароль остаётся в памяти формы (и уходит вместе с кодом).
const needCode = ref(false)
const loginField = ref<InstanceType<typeof UiInput> | null>(null)
const passwordField = ref<InstanceType<typeof UiInput> | null>(null)
const totpField = ref<InstanceType<typeof UiInput> | null>(null)

async function focusFirstProblem() {
  await nextTick()
  if (needCode.value) totpField.value?.focus()
  else if (form.errors.login) loginField.value?.focus()
  else passwordField.value?.focus()
}

/** «Назад» со второго шага: другой логин или пароль. */
function backToPassword() {
  needCode.value = false
  totp.value = ''
  password.value = ''
  form.clear()
  void focusFirstProblem()
}

async function onSubmit() {
  form.clear()
  if (!login.value.trim()) form.errors.login = t('common.enterLogin')
  if (!password.value) form.errors.password = t('common.enterPassword')
  if (needCode.value && !totp.value.trim()) form.errors.totp = t('login.needCode')
  if (Object.keys(form.errors).length > 0) return focusFirstProblem()

  // Куда вернуть после входа — запоминаем до отправки: адрес может измениться, пока идёт запрос.
  const target = safeNextPath(route.query.next) || safeNextPath(ui.authNext) || { name: 'file' }
  const ok = await form.submit(() => auth.login(login.value.trim(), password.value, totp.value.trim()))
  if (ok) {
    // Переход делаем здесь, а не событием родителю: после входа главная перерисовывается
    // и размонтирует форму, а Vue не доставляет события размонтированных компонентов.
    await router.push(target)
    // Возврат на тот же адрес (закрытый документ, с которого нажали «Войти») перехода не даёт — окно закрываем сами.
    ui.closeAuth()
    return undefined
  }
  if (form.lastError.value?.code === 'totp_required') {
    // не ошибка: пароль верен, нужен второй шаг
    needCode.value = true
    form.formError.value = ''
    return focusFirstProblem()
  }
  if (needCode.value) {
    // неверный код: пароль остаётся, код вводят заново
    totp.value = ''
    return focusFirstProblem()
  }
  password.value = ''
  return focusFirstProblem()
}
</script>

<template>
  <form novalidate :aria-label="$t('login.form')" @submit.prevent="onSubmit">
    <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
    <template v-if="!needCode">
      <UiField id="login-login" :label="$t('common.login')" :error="form.errors.login">
        <UiInput ref="loginField" v-model="login" autocomplete="username" />
      </UiField>
      <UiField id="login-password" :label="$t('common.password')" :error="form.errors.password">
        <UiInput ref="passwordField" v-model="password" type="password" autocomplete="current-password" reveal />
      </UiField>
    </template>
    <template v-else>
      <p class="second" data-testid="login-second-step">{{ $t('login.secondStep', { login: login.trim() }) }}</p>
      <UiField id="login-totp" :label="$t('login.codeLabel')" :hint="$t('login.codeHint')" :error="form.errors.totp">
        <UiInput ref="totpField" v-model="totp" inputmode="numeric" autocomplete="one-time-code" :maxlength="16" />
      </UiField>
    </template>
    <div class="actions">
      <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ form.submitting.value ? $t('common.checking') : $t('login.submit') }}</UiButton>
      <UiButton v-if="needCode" variant="link" data-testid="login-back" @click="backToPassword">{{ $t('common.back') }}</UiButton>
      <UiButton v-else to="/restore" variant="link">{{ $t('login.forgot') }}</UiButton>
    </div>
  </form>
</template>

<style scoped>
.second {
  margin-top: 0;
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
}
</style>
