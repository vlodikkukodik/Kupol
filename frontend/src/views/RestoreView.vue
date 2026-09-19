<script setup>
import { nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import FormField from '../components/FormField.vue'
import { useForm } from '../composables/useForm.js'
import { validatePassword } from '../lib/rules.js'
import { useAuthStore } from '../stores/auth.js'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const login = ref('')
const code = ref('')
const password = ref('')
const password2 = ref('')

const fieldEls = {}
const setFieldRef = (name) => (el) => {
  fieldEls[name] = el
}
const order = ['login', 'backup_code', 'new_password', 'new_password2']

async function focusFirstError() {
  await nextTick()
  const first = order.find((name) => form.errors[name])
  if (first) fieldEls[first]?.focus()
}

async function onSubmit() {
  form.clear()
  if (!login.value.trim()) form.errors.login = 'Введите логин'
  if (!code.value.trim()) form.errors.backup_code = 'Введите резервный код'
  const err = validatePassword(password.value, login.value.trim())
  if (err) form.errors.new_password = err
  else if (password.value !== password2.value) form.errors.new_password2 = 'Пароли не совпадают'
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  const ok = await form.submit(() =>
    auth.restore({ login: login.value.trim(), backupCode: code.value, newPassword: password.value }),
  )
  if (ok) {
    await router.push({ name: 'backup-code' })
    return undefined
  }
  return focusFirstError()
}
</script>

<template>
  <article>
    <h1>Восстановление доступа</h1>
    <p>
      Введите логин, резервный код, полученный при регистрации, и новый пароль. После восстановления все прежние сеансы
      завершатся, а резервный код будет заменён на новый.
    </p>

    <form class="form" novalidate aria-label="Восстановление доступа" @submit.prevent="onSubmit">
      <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
      <FormField
        id="restore-login"
        :ref="setFieldRef('login')"
        v-model="login"
        label="Логин"
        autocomplete="username"
        :error="form.errors.login"
      />
      <FormField
        id="restore-code"
        :ref="setFieldRef('backup_code')"
        v-model="code"
        label="Резервный код"
        autocomplete="off"
        hint="Вида KUPOL-XXXX-XXXX-XXXX-XXXX; регистр и дефисы не важны."
        :error="form.errors.backup_code"
      />
      <FormField
        id="restore-password"
        :ref="setFieldRef('new_password')"
        v-model="password"
        label="Новый пароль"
        type="password"
        autocomplete="new-password"
        hint="Не короче 8 символов."
        :error="form.errors.new_password"
      />
      <FormField
        id="restore-password2"
        :ref="setFieldRef('new_password2')"
        v-model="password2"
        label="Новый пароль ещё раз"
        type="password"
        autocomplete="new-password"
        :error="form.errors.new_password2"
      />
      <div class="form-actions">
        <button type="submit" class="btn" :disabled="form.submitting.value">
          {{ form.submitting.value ? 'Проверка…' : 'Восстановить доступ' }}
        </button>
        <RouterLink to="/" class="form-link">Назад</RouterLink>
      </div>
    </form>
    <p class="note">Потеряли и код? Обратитесь к Директорату: пароль сбросит администратор.</p>
  </article>
</template>

<style scoped>
.note { margin-top: var(--space-4); font-size: 0.9rem; color: var(--ink-soft); }
</style>
