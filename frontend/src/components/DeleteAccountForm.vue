<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import { useForm } from '@/composables/useForm'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const form = useForm()

const open = ref(false)
const password = ref('')
const confirmed = ref(false)
const passwordField = ref<InstanceType<typeof UiInput> | null>(null)
const confirmBox = ref<InstanceType<typeof UiCheckbox> | null>(null)

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
    <p>Аккаунт и все связанные с ним данные будут удалены безвозвратно. Логин освободится. Восстановить дело будет невозможно.</p>
    <UiButton v-if="!open" variant="danger" aria-expanded="false" icon="trash" @click="open = true">Сдать дело…</UiButton>

    <form v-else novalidate aria-label="Удаление аккаунта" @submit.prevent="onSubmit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiField id="del-password" label="Пароль для подтверждения" :error="form.errors.current_password">
        <UiInput ref="passwordField" v-model="password" type="password" autocomplete="current-password" />
      </UiField>
      <UiCheckbox
        id="del-confirm"
        ref="confirmBox"
        v-model="confirmed"
        label="Я понимаю: аккаунт и все мои данные будут удалены навсегда"
        :invalid="Boolean(form.errors.confirm)"
        :describedby="form.errors.confirm ? 'del-confirm-error' : undefined"
      />
      <p v-if="form.errors.confirm" id="del-confirm-error" class="error">{{ form.errors.confirm }}</p>
      <div class="actions">
        <UiButton type="submit" variant="danger" :loading="form.submitting.value">{{ form.submitting.value ? 'Удаление…' : 'Удалить аккаунт навсегда' }}</UiButton>
        <UiButton variant="link" @click="open = false">Отмена</UiButton>
      </div>
    </form>
  </section>
</template>

<style scoped>
.danger h3 {
  color: var(--red-800);
}
.error {
  margin: var(--space-1) 0 0;
  color: var(--danger);
  font-size: var(--text-sm);
  font-weight: 700;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
  margin-top: var(--space-4);
}
</style>
