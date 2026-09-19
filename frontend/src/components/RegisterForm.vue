<script setup>
import { nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import FormField from './FormField.vue'
import { api } from '../api/index.js'
import { ApiError } from '../api/client.js'
import { describeApiError, useForm } from '../composables/useForm.js'
import { validateLogin, validatePassword } from '../lib/rules.js'
import { useAuthStore } from '../stores/auth.js'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const login = ref('')
const password = ref('')
const password2 = ref('')
const answer = ref('')
const captcha = ref({ id: '', question: '' })
const captchaLoading = ref(false)

// Компоненты полей нужны, чтобы перевести фокус на первое поле с ошибкой.
const fieldEls = {}
const setFieldRef = (name) => (el) => {
  fieldEls[name] = el
}
const order = ['login', 'password', 'password2', 'captcha_answer']

async function loadCaptcha() {
  captchaLoading.value = true
  try {
    const res = await api.get('/auth/captcha')
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
  else if (password2.value !== password.value) form.errors.password2 = 'Пароли не совпадают'
  if (!captcha.value.id) form.formError.value = 'Вопрос анкеты не загружен. Обновите его и повторите.'
  else if (!answer.value.trim()) form.errors.captcha_answer = 'Ответьте на вопрос анкеты'
  if (Object.keys(form.errors).length > 0 || form.formError.value) return focusFirstError()

  const ok = await form.submit(() =>
    auth.register({
      login: login.value,
      password: password.value,
      captchaId: captcha.value.id,
      captchaAnswer: answer.value,
    }),
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
  <form class="form" novalidate aria-label="Регистрация" @submit.prevent="onSubmit">
    <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
    <FormField
      id="reg-login"
      :ref="setFieldRef('login')"
      v-model="login"
      label="Логин"
      autocomplete="username"
      hint="3–24 символа: буквы (латиница или кириллица, не вперемешку), цифры, «_» и «-». Без почты."
      :maxlength="24"
      :error="form.errors.login"
    />
    <FormField
      id="reg-password"
      :ref="setFieldRef('password')"
      v-model="password"
      label="Пароль"
      type="password"
      autocomplete="new-password"
      hint="Не короче 8 символов."
      :error="form.errors.password"
    />
    <FormField
      id="reg-password2"
      :ref="setFieldRef('password2')"
      v-model="password2"
      label="Пароль ещё раз"
      type="password"
      autocomplete="new-password"
      :error="form.errors.password2"
    />

    <fieldset class="captcha">
      <legend>Анкета</legend>
      <p class="captcha-question" :aria-busy="captchaLoading ? 'true' : 'false'">
        {{ captchaLoading ? 'Загрузка вопроса…' : captcha.question || 'Вопрос недоступен' }}
      </p>
      <FormField
        id="reg-captcha"
        :ref="setFieldRef('captcha_answer')"
        v-model="answer"
        label="Ваш ответ"
        autocomplete="off"
        :error="form.errors.captcha_answer"
      />
      <button type="button" class="form-link" :disabled="captchaLoading" @click="loadCaptcha">Другой вопрос</button>
    </fieldset>

    <div class="form-actions">
      <button type="submit" class="btn" :disabled="form.submitting.value || captchaLoading">
        {{ form.submitting.value ? 'Оформление…' : 'Оформить допуск' }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.captcha {
  margin: 0 0 var(--space-3);
  padding: var(--space-3);
  border: 1px dashed var(--ink-soft);
}
.captcha legend {
  padding: 0 var(--space-2);
  font-family: var(--font-head);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.captcha-question { margin: 0 0 var(--space-3); font-weight: 700; }
</style>
