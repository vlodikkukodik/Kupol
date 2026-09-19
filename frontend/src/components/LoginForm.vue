<script setup>
import { nextTick, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import FormField from './FormField.vue'
import { useForm } from '../composables/useForm.js'
import { safeNextPath } from '../router/index.js'
import { useAuthStore } from '../stores/auth.js'
import { useUiStore } from '../stores/ui.js'

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()
const form = useForm()
const login = ref('')
const password = ref('')
const loginField = ref(null)
const passwordField = ref(null)

async function focusFirstProblem() {
  await nextTick()
  if (form.errors.login) loginField.value?.focus()
  else passwordField.value?.focus()
}

async function onSubmit() {
  form.clear()
  if (!login.value.trim()) form.errors.login = 'Введите логин'
  if (!password.value) form.errors.password = 'Введите пароль'
  if (Object.keys(form.errors).length > 0) return focusFirstProblem()

  // Куда вернуть после входа — запоминаем до отправки: адрес может измениться, пока идёт запрос.
  const target = safeNextPath(route.query.next) || safeNextPath(ui.authNext) || { name: 'file' }
  const ok = await form.submit(() => auth.login(login.value.trim(), password.value))
  if (ok) {
    // Переход делаем здесь, а не событием родителю: после входа главная перерисовывается
    // и размонтирует форму, а Vue не доставляет события размонтированных компонентов.
    await router.push(target)
    // Возврат на тот же адрес (закрытый документ, с которого нажали «Войти») перехода не даёт — окно закрываем сами.
    ui.closeAuth()
    return undefined
  }
  password.value = ''
  return focusFirstProblem()
}
</script>

<template>
  <form class="form" novalidate aria-label="Вход" @submit.prevent="onSubmit">
    <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
    <FormField
      id="login-login"
      ref="loginField"
      v-model="login"
      label="Логин"
      autocomplete="username"
      :error="form.errors.login"
    />
    <FormField
      id="login-password"
      ref="passwordField"
      v-model="password"
      label="Пароль"
      type="password"
      autocomplete="current-password"
      :error="form.errors.password"
    />
    <div class="form-actions">
      <button type="submit" class="btn" :disabled="form.submitting.value">
        {{ form.submitting.value ? 'Проверка…' : 'Войти' }}
      </button>
      <RouterLink to="/restore" class="form-link">Забыли пароль?</RouterLink>
    </div>
  </form>
</template>
