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
import { t } from '@/i18n'
import SecretCodesBox from '@/components/SecretCodesBox.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()
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
  if (!login.value.trim()) form.errors.login = t('common.enterLogin')
  if (!code.value.trim()) form.errors.backup_code = t('restore.enterCode')
  const err = validatePassword(password.value, login.value.trim())
  if (err) form.errors.new_password = err
  else if (password.value !== password2.value) form.errors.new_password2 = t('common.passwordsDiffer')
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  let result: { signedIn: boolean; backupCode: string } | null = null
  const ok = await form.submit(async () => {
    result = await auth.restore({ login: login.value.trim(), backupCode: code.value, newPassword: password.value })
  })
  if (ok && result) {
    const done = result as { signedIn: boolean; backupCode: string }
    if (done.signedIn) {
      await router.push({ name: 'backup-code' })
      return undefined
    }
    // Включён код из приложения: пароль сменён, но сессии нет. Новый резервный код показываем здесь, вход — обычный.
    restored.value = { login: login.value.trim(), backupCode: done.backupCode }
    password.value = password2.value = code.value = ''
    return undefined
  }
  return focusFirstError()
}

// Без сессии (у человека включён код из приложения) новый резервный код показывается на этом же экране.
const restored = ref<{ login: string; backupCode: string } | null>(null)
const saved = ref(false)

function signIn() {
  restored.value = null
  ui.openAuth()
}
</script>

<template>
  <UiSheet v-if="restored" as="article" class="restore" data-testid="restore-done">
    <h1>{{ $t('restore.doneTitle') }}</h1>
    <p>
      <i18n-t keypath="restore.doneText" scope="global"><template #once><strong>{{ $t('restore.once') }}</strong></template></i18n-t>
    </p>
    <SecretCodesBox
      :codes="[restored.backupCode]"
      :what="$t('restore.codesWhat')"
      :login="restored.login"
      filename="kupol-backup-code.txt"
      :note="$t('restore.codesNote')"
    />
    <div class="confirm">
      <UiCheckbox v-model="saved" :label="$t('common.savedCodes')" />
    </div>
    <UiButton variant="primary" :disabled="!saved" icon-end="check" data-testid="restore-sign-in" @click="signIn">{{ $t('restore.signIn') }}</UiButton>
  </UiSheet>

  <UiSheet v-else as="article" class="restore">
    <h1>{{ $t('restore.title') }}</h1>
    <p>{{ $t('restore.intro') }}</p>

    <form novalidate :aria-label="$t('restore.title')" @submit.prevent="onSubmit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiField id="restore-login" :label="$t('common.login')" :error="form.errors.login">
        <UiInput :ref="setFieldRef('login')" v-model="login" autocomplete="username" />
      </UiField>
      <UiField id="restore-code" :label="$t('restore.codeLabel')" :hint="$t('restore.codeHint')" :error="form.errors.backup_code">
        <UiInput :ref="setFieldRef('backup_code')" v-model="code" autocomplete="off" />
      </UiField>
      <UiField id="restore-password" :label="$t('common.newPassword')" :hint="$t('common.passwordMin')" :error="form.errors.new_password">
        <UiInput :ref="setFieldRef('new_password')" v-model="password" type="password" autocomplete="new-password" reveal />
      </UiField>
      <UiField id="restore-password2" :label="$t('common.newPasswordAgain')" :error="form.errors.new_password2">
        <UiInput :ref="setFieldRef('new_password2')" v-model="password2" type="password" autocomplete="new-password" />
      </UiField>
      <div class="actions">
        <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ form.submitting.value ? $t('common.checking') : $t('restore.submit') }}</UiButton>
        <UiButton to="/" variant="link">{{ $t('common.back') }}</UiButton>
      </div>
    </form>
    <p class="note">{{ $t('restore.lostBoth') }}</p>
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
.confirm {
  margin: var(--space-4) 0 var(--space-3);
}
.note {
  margin: var(--space-5) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
