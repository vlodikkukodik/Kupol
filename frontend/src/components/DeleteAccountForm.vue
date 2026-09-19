<script setup>
import { nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import FormField from './FormField.vue'
import { useForm } from '../composables/useForm.js'
import { useAuthStore } from '../stores/auth.js'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const open = ref(false)
const password = ref('')
const confirmed = ref(false)
const passwordField = ref(null)
const confirmBox = ref(null)

async function onSubmit() {
  form.clear()
  if (!password.value) form.errors.current_password = 'Введите пароль для подтверждения'
  if (!confirmed.value) form.errors.confirm = 'Подтвердите, что понимаете последствия'
  if (Object.keys(form.errors).length > 0) {
    await nextTick()
    if (form.errors.current_password) passwordField.value?.focus()
    else confirmBox.value?.focus()
    return
  }
  const ok = await form.submit(() => auth.deleteAccount(password.value))
  if (ok) {
    await router.replace({ name: 'home' })
    return
  }
  password.value = ''
  await nextTick()
  passwordField.value?.focus()
}
</script>

<template>
  <section class="danger" aria-labelledby="danger-title">
    <h3 id="danger-title">Сдать дело в архив</h3>
    <p>
      Аккаунт и все связанные с ним данные будут удалены безвозвратно. Логин освободится. Восстановить дело будет
      невозможно.
    </p>
    <button v-if="!open" type="button" class="btn btn--danger" aria-expanded="false" @click="open = true">
      Сдать дело…
    </button>

    <form v-else class="form" novalidate aria-label="Удаление аккаунта" @submit.prevent="onSubmit">
      <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
      <FormField
        id="del-password"
        ref="passwordField"
        v-model="password"
        label="Пароль для подтверждения"
        type="password"
        autocomplete="current-password"
        :error="form.errors.current_password"
      />
      <div class="check" :class="{ 'check--invalid': form.errors.confirm }">
        <input
          id="del-confirm"
          ref="confirmBox"
          v-model="confirmed"
          type="checkbox"
          :aria-invalid="form.errors.confirm ? 'true' : undefined"
          :aria-describedby="form.errors.confirm ? 'del-confirm-error' : undefined"
        >
        <label for="del-confirm">Я понимаю: аккаунт и все мои данные будут удалены навсегда</label>
      </div>
      <p v-if="form.errors.confirm" id="del-confirm-error" class="error">{{ form.errors.confirm }}</p>
      <div class="form-actions">
        <button type="submit" class="btn btn--danger" :disabled="form.submitting.value">
          {{ form.submitting.value ? 'Удаление…' : 'Удалить аккаунт навсегда' }}
        </button>
        <button type="button" class="form-link" @click="open = false">Отмена</button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.danger {
  margin-top: var(--space-5);
  padding: var(--space-4);
  border: 2px solid var(--stamp-red);
}
.danger h3 { color: var(--stamp-red); }
.check { display: flex; gap: var(--space-2); align-items: flex-start; margin-bottom: var(--space-3); }
.check input { flex: none; width: 1.2rem; height: 1.2rem; margin-top: 0.2rem; accent-color: var(--stamp-red); }
.check label { cursor: pointer; }
.error { margin: calc(-1 * var(--space-2)) 0 var(--space-3); color: var(--stamp-red); font-weight: 700; font-size: 0.9rem; }
</style>
