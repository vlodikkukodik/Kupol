<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSheet from '@/ui/UiSheet.vue'
import { useForm } from '@/composables/useForm'
import { validatePassword } from '@/lib/rules'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const login = ref('')
const code = ref('')
const password = ref('')
const password2 = ref('')

const fieldEls: Record<string, InstanceType<typeof UiInput> | null> = {}
const setFieldRef = (name: string) => (el: unknown) => {
  fieldEls[name] = el as InstanceType<typeof UiInput> | null
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

  const ok = await form.submit(() => auth.restore({ login: login.value.trim(), backupCode: code.value, newPassword: password.value }))
  if (ok) {
    await router.push({ name: 'backup-code' })
    return undefined
  }
  return focusFirstError()
}
</script>

<template>
  <UiSheet as="article" class="restore">
    <h1>Восстановление доступа</h1>
    <p>
      Введите логин, резервный код, полученный при регистрации, и новый пароль. После восстановления все прежние сеансы
      завершатся, а резервный код будет заменён на новый.
    </p>

    <form novalidate aria-label="Восстановление доступа" @submit.prevent="onSubmit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiField id="restore-login" label="Логин" :error="form.errors.login">
        <UiInput :ref="setFieldRef('login')" v-model="login" autocomplete="username" />
      </UiField>
      <UiField id="restore-code" label="Резервный код" hint="Вида KUPOL-XXXX-XXXX-XXXX-XXXX; регистр и дефисы не важны." :error="form.errors.backup_code">
        <UiInput :ref="setFieldRef('backup_code')" v-model="code" autocomplete="off" />
      </UiField>
      <UiField id="restore-password" label="Новый пароль" hint="Не короче 8 символов." :error="form.errors.new_password">
        <UiInput :ref="setFieldRef('new_password')" v-model="password" type="password" autocomplete="new-password" reveal />
      </UiField>
      <UiField id="restore-password2" label="Новый пароль ещё раз" :error="form.errors.new_password2">
        <UiInput :ref="setFieldRef('new_password2')" v-model="password2" type="password" autocomplete="new-password" />
      </UiField>
      <div class="actions">
        <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ form.submitting.value ? 'Проверка…' : 'Восстановить доступ' }}</UiButton>
        <UiButton to="/" variant="link">Назад</UiButton>
      </div>
    </form>
    <p class="note">Потеряли и код? Обратитесь к Директорату: пароль сбросит администратор.</p>
  </UiSheet>
</template>

<style scoped>
.restore {
  max-width: 40rem;
  margin-top: var(--space-4);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
}
.note {
  margin: var(--space-5) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
