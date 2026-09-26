<script setup lang="ts">
// Почта в личном деле — по желанию (шаг 5.1.1): указанный адрес становится действующим только после перехода
// по ссылке из письма (EmailConfirmView.vue). auth.user.email/pending_email обновляются через refresh() после
// каждого действия — отдельного запроса состояния, в отличие от TotpPanel, не нужно: сервер уже прислал их в сессии.
import { computed, ref } from 'vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import { authApi } from '@/api/endpoints'
import { useForm } from '@/composables/useForm'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const form = useForm()

type Mode = 'set' | 'remove' | null
const mode = ref<Mode>(null)
const password = ref('')
const email = ref('')
const notice = ref('')

const open = computed({
  get: () => mode.value !== null,
  set: (v) => {
    if (!v) reset()
  },
})

function reset() {
  mode.value = null
  password.value = ''
  email.value = auth.user?.email ?? auth.user?.pending_email ?? ''
  form.clear()
}

function start(m: Exclude<Mode, null>) {
  reset()
  mode.value = m
  notice.value = ''
}

async function refresh() {
  await auth.load(true)
}

async function submitSet() {
  form.clear()
  if (!email.value.trim()) form.errors.email = t('email.enterAddress')
  if (!password.value) form.errors.current_password = t('email.enterPassword')
  if (Object.keys(form.errors).length) return
  const ok = await form.submit(async () => {
    await authApi.setEmail({ password: password.value, email: email.value.trim() })
  })
  if (ok) {
    reset()
    notice.value = t('email.sentNotice')
    await refresh()
  }
}

async function submitRemove() {
  form.clear()
  if (!password.value) {
    form.errors.current_password = t('email.enterPassword')
    return
  }
  const ok = await form.submit(async () => {
    await authApi.removeEmail(password.value)
  })
  if (ok) {
    reset()
    notice.value = t('email.removedNotice')
    await refresh()
  }
}
</script>

<template>
  <div class="email" data-testid="email">
    <template v-if="auth.user?.email">
      <p class="state state--on" data-testid="email-state">{{ $t('email.confirmed', { email: auth.user.email }) }}</p>
      <div class="actions">
        <UiButton data-testid="email-change" @click="start('set')">{{ $t('email.change') }}</UiButton>
        <UiButton variant="ghost" data-testid="email-remove" @click="start('remove')">{{ $t('email.remove') }}</UiButton>
      </div>
    </template>
    <template v-else-if="auth.user?.pending_email">
      <p class="state" data-testid="email-state">{{ $t('email.pending', { email: auth.user.pending_email }) }}</p>
      <div class="actions">
        <UiButton data-testid="email-resend" @click="start('set')">{{ $t('email.resend') }}</UiButton>
        <UiButton variant="ghost" data-testid="email-remove" @click="start('remove')">{{ $t('email.cancelPending') }}</UiButton>
      </div>
    </template>
    <template v-else>
      <p class="state" data-testid="email-state">{{ $t('email.none') }}</p>
      <UiButton icon="mail" data-testid="email-add" @click="start('set')">{{ $t('email.add') }}</UiButton>
    </template>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice && !open" class="notice" data-testid="email-notice">{{ notice }}</p>

    <UiModal v-model:open="open" :title="mode === 'remove' ? $t('email.removeTitle') : $t('email.setTitle')" size="md" testid="email-dialog">
      <form v-if="mode === 'set'" novalidate @submit.prevent="submitSet">
        <p>{{ $t('email.setIntro') }}</p>
        <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
        <UiField id="email-address" :label="$t('email.label')" :error="form.errors.email">
          <UiInput v-model="email" type="email" autocomplete="email" />
        </UiField>
        <UiField id="email-password" :label="$t('common.password')" :error="form.errors.current_password">
          <UiInput v-model="password" type="password" autocomplete="current-password" reveal />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="email-submit">{{ $t('email.setSubmit') }}</UiButton>
          <UiButton variant="link" @click="open = false">{{ $t('email.dismiss') }}</UiButton>
        </div>
      </form>
      <form v-else-if="mode === 'remove'" novalidate @submit.prevent="submitRemove">
        <p>{{ $t('email.removeIntro') }}</p>
        <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
        <UiField id="email-remove-password" :label="$t('common.password')" :error="form.errors.current_password">
          <UiInput v-model="password" type="password" autocomplete="current-password" reveal />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="danger" :loading="form.submitting.value" data-testid="email-remove-submit">{{ $t('email.removeSubmit') }}</UiButton>
          <UiButton variant="link" @click="open = false">{{ $t('email.dismiss') }}</UiButton>
        </div>
      </form>
    </UiModal>
  </div>
</template>

<style scoped>
.state {
  margin-top: 0;
}
.state--on {
  font-weight: 700;
}
.actions,
.dlg-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
  margin-top: var(--space-3);
}
.notice {
  margin: var(--space-3) 0 0;
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--success);
  background: var(--surface-sunken);
}
</style>
