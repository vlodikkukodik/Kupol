<script setup lang="ts">
import { nextTick, ref } from 'vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import { useForm } from '@/composables/useForm'
import { validatePassword } from '@/lib/rules'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const form = useForm()

const current = ref('')
const next = ref('')
const next2 = ref('')
const done = ref(false)

const fieldEls: Record<string, InstanceType<typeof UiInput> | null> = {}
const setFieldRef = (name: string) => (el: unknown) => {
  fieldEls[name] = el as InstanceType<typeof UiInput> | null
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
  if (!current.value) form.errors.current_password = t('password.enterCurrent')
  const err = validatePassword(next.value, auth.user?.login)
  if (err) form.errors.new_password = err
  else if (next.value !== next2.value) form.errors.new_password2 = t('common.passwordsDiffer')
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  const ok = await form.submit(() => auth.changePassword(current.value, next.value))
  if (ok) {
    done.value = true
    current.value = next.value = next2.value = ''
    return undefined
  }
  return focusFirstError()
}
</script>

<template>
  <form novalidate :aria-label="$t('password.form')" @submit.prevent="onSubmit">
    <!-- Скрытое поле логина: менеджеры паролей связывают смену пароля с нужной записью -->
    <input type="text" class="visually-hidden" tabindex="-1" autocomplete="username" :aria-label="$t('password.hiddenLogin')" :value="auth.user?.login" readonly>
    <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
    <UiAlert v-if="done" tone="success">{{ $t('password.done') }}</UiAlert>
    <UiField id="pw-current" :label="$t('password.current')" :error="form.errors.current_password">
      <UiInput :ref="setFieldRef('current_password')" v-model="current" type="password" autocomplete="current-password" />
    </UiField>
    <UiField id="pw-new" :label="$t('common.newPassword')" :hint="$t('common.passwordMin')" :error="form.errors.new_password">
      <UiInput :ref="setFieldRef('new_password')" v-model="next" type="password" autocomplete="new-password" reveal />
    </UiField>
    <UiField id="pw-new2" :label="$t('common.newPasswordAgain')" :error="form.errors.new_password2">
      <UiInput :ref="setFieldRef('new_password2')" v-model="next2" type="password" autocomplete="new-password" />
    </UiField>
    <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ form.submitting.value ? $t('password.submitting') : $t('password.submit') }}</UiButton>
  </form>
</template>
