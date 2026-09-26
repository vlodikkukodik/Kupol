<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import { ApiError } from '@/api/client'
import { authApi } from '@/api/endpoints'
import { describeApiError, useForm } from '@/composables/useForm'
import { t } from '@/i18n'
import { validateLogin, validatePassword } from '@/lib/rules'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const login = ref('')
const password = ref('')
const password2 = ref('')
const answer = ref('')
const captcha = ref({ id: '', question: '' })
const captchaLoading = ref(false)

// Поля нужны, чтобы перевести фокус на первое поле с ошибкой.
const fieldEls: Record<string, InstanceType<typeof UiInput> | null> = {}
const setFieldRef = (name: string) => (el: unknown) => {
  fieldEls[name] = el as InstanceType<typeof UiInput> | null
}
const order = ['login', 'password', 'password2', 'captcha_answer']

async function loadCaptcha() {
  captchaLoading.value = true
  try {
    const res = await authApi.captcha()
    captcha.value = { id: res.id, question: res.question }
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    captcha.value = { id: '', question: '' }
    form.formError.value = describeApiError(err)
  } finally {
    captchaLoading.value = false
  }
}

onMounted(loadCaptcha)

async function focusFirstError() {
  await nextTick()
  const first = order.find((name) => form.errors[name])
  if (first) fieldEls[first]?.focus()
}

async function onSubmit() {
  form.clear()
  const loginErr = validateLogin(login.value)
  if (loginErr) form.errors.login = loginErr
  const passErr = validatePassword(password.value, login.value)
  if (passErr) form.errors.password = passErr
  else if (password2.value !== password.value) form.errors.password2 = t('common.passwordsDiffer')
  if (!captcha.value.id) form.formError.value = t('register.captchaMissing')
  else if (!answer.value.trim()) form.errors.captcha_answer = t('register.captchaAnswerNeeded')
  if (Object.keys(form.errors).length > 0 || form.formError.value) return focusFirstError()

  const ok = await form.submit(() =>
    auth.register({ login: login.value, password: password.value, captchaId: captcha.value.id, captchaAnswer: answer.value }),
  )
  if (ok) {
    // Переход делаем здесь, а не событием родителю: как только в хранилище появился пользователь,
    // главная перерисовывается и размонтирует эту форму, а Vue не доставляет события размонтированных компонентов.
    await router.push({ name: 'backup-code' })
    return undefined
  }
  // Ошибки полей формы (validation) вопрос не тратят; любая другая неудача после проверки анкеты — тратит.
  if (form.lastError.value?.code !== 'validation') {
    answer.value = ''
    await loadCaptcha()
  }
  return focusFirstError()
}
</script>

<template>
  <form novalidate :aria-label="$t('register.form')" @submit.prevent="onSubmit">
    <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
    <UiField id="reg-login" :label="$t('common.login')" :hint="$t('register.loginHint')" :error="form.errors.login">
      <UiInput :ref="setFieldRef('login')" v-model="login" autocomplete="username" :maxlength="24" />
    </UiField>
    <UiField id="reg-password" :label="$t('common.password')" :hint="$t('common.passwordMin')" :error="form.errors.password">
      <UiInput :ref="setFieldRef('password')" v-model="password" type="password" autocomplete="new-password" reveal />
    </UiField>
    <UiField id="reg-password2" :label="$t('common.passwordAgain')" :error="form.errors.password2">
      <UiInput :ref="setFieldRef('password2')" v-model="password2" type="password" autocomplete="new-password" />
    </UiField>

    <fieldset class="captcha">
      <legend>{{ $t('register.questionnaire') }}</legend>
      <p class="captcha__question" :aria-busy="captchaLoading ? 'true' : 'false'">
        {{ captchaLoading ? $t('register.loadingQuestion') : captcha.question || $t('register.questionUnavailable') }}
      </p>
      <UiField id="reg-captcha" :label="$t('register.yourAnswer')" :error="form.errors.captcha_answer">
        <UiInput :ref="setFieldRef('captcha_answer')" v-model="answer" autocomplete="off" />
      </UiField>
      <UiButton variant="link" size="sm" :disabled="captchaLoading" @click="loadCaptcha">{{ $t('register.anotherQuestion') }}</UiButton>
    </fieldset>

    <div class="actions">
      <UiButton type="submit" variant="primary" :loading="form.submitting.value || captchaLoading">
        {{ form.submitting.value ? $t('register.submitting') : $t('register.submit') }}
      </UiButton>
    </div>
  </form>
</template>

<style scoped>
.captcha {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-2);
  border: 2px dashed var(--border-strong);
  border-radius: var(--radius-2);
}
.captcha legend {
  padding: 0 var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.captcha__question {
  margin: 0 0 var(--space-3);
  font-weight: 700;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
</style>
