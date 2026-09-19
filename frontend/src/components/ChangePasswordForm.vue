<script setup>
import { nextTick, ref } from 'vue'
import FormField from './FormField.vue'
import { useForm } from '../composables/useForm.js'
import { validatePassword } from '../lib/rules.js'
import { useAuthStore } from '../stores/auth.js'

const auth = useAuthStore()
const form = useForm()

const current = ref('')
const next = ref('')
const next2 = ref('')
const done = ref(false)

const fieldEls = {}
const setFieldRef = (name) => (el) => {
  fieldEls[name] = el
}
const order = ['current_password', 'new_password', 'new_password2']

async function focusFirstError() {
  await nextTick()
  const first = order.find((name) => form.errors[name])
  if (first) fieldEls[first]?.focus()
}

async function onSubmit() {
  form.clear()
  done.value = false
  if (!current.value) form.errors.current_password = 'Введите текущий пароль'
  const err = validatePassword(next.value, auth.user?.login)
  if (err) form.errors.new_password = err
  else if (next.value !== next2.value) form.errors.new_password2 = 'Пароли не совпадают'
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  const ok = await form.submit(() => auth.changePassword(current.value, next.value), { new_password: 'new_password' })
  if (ok) {
    done.value = true
    current.value = next.value = next2.value = ''
    return undefined
  }
  return focusFirstError()
}
</script>

<template>
  <form class="form" novalidate aria-label="Смена пароля" @submit.prevent="onSubmit">
    <!-- Скрытое поле логина: менеджеры паролей связывают смену пароля с нужной записью -->
    <input
      type="text"
      class="visually-hidden"
      tabindex="-1"
      autocomplete="username"
      aria-label="Логин (подставляется автоматически)"
      :value="auth.user?.login"
      readonly
    >
    <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
    <p v-if="done" class="form-ok" role="status">Пароль изменён. Остальные ваши сеансы завершены.</p>
    <FormField
      id="pw-current"
      :ref="setFieldRef('current_password')"
      v-model="current"
      label="Текущий пароль"
      type="password"
      autocomplete="current-password"
      :error="form.errors.current_password"
    />
    <FormField
      id="pw-new"
      :ref="setFieldRef('new_password')"
      v-model="next"
      label="Новый пароль"
      type="password"
      autocomplete="new-password"
      hint="Не короче 8 символов."
      :error="form.errors.new_password"
    />
    <FormField
      id="pw-new2"
      :ref="setFieldRef('new_password2')"
      v-model="next2"
      label="Новый пароль ещё раз"
      type="password"
      autocomplete="new-password"
      :error="form.errors.new_password2"
    />
    <div class="form-actions">
      <button type="submit" class="btn" :disabled="form.submitting.value">
        {{ form.submitting.value ? 'Сохранение…' : 'Сменить пароль' }}
      </button>
    </div>
  </form>
</template>
