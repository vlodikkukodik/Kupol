<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import QrCode from '@/components/QrCode.vue'
import SecretCodesBox from '@/components/SecretCodesBox.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import type { TOTPSetupResponse } from '@/api/generated/httpapi'
import { totpApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { useForm } from '@/composables/useForm'
import { groupSecret } from '@/lib/totp'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

// Код из приложения (TOTP) — по желанию: вход без него продолжает работать, пока человек сам не включит защиту.
// Подключение в три шага: пароль → QR (или секрет вручную) и первый код → одноразовые коды на случай потери телефона.
// Защита включается только первым верным кодом: ошибка при сканировании не запирает человека за его собственным паролем.
const auth = useAuthStore()
const client = useQueryClient()
const form = useForm()

const status = useQuery({ queryKey: keys.totp, queryFn: ({ signal }) => totpApi.status({ signal }), staleTime: 0 })

type Mode = 'enable' | 'renew' | 'disable'
type Step = 'password' | 'scan' | 'confirm' | 'codes'
const mode = ref<Mode | null>(null)
const step = ref<Step>('password')
const password = ref('')
const code = ref('')
const setup = ref<TOTPSetupResponse | null>(null)
const codes = ref<string[]>([])
const saved = ref(false)
const closeHint = ref('')
const notice = ref('')

const open = computed({
  get: () => mode.value !== null,
  set: (v) => {
    if (v) return
    // Одноразовые коды показываются один раз: окно с ними не закрывается, пока их не сохранили.
    if (step.value === 'codes' && !saved.value) {
      closeHint.value = t('totp.closeHint')
      return
    }
    reset()
  },
})

function reset() {
  mode.value = null
  step.value = 'password'
  password.value = ''
  code.value = ''
  setup.value = null
  codes.value = []
  saved.value = false
  closeHint.value = ''
  form.clear()
}

function start(m: Mode) {
  reset()
  mode.value = m
  step.value = m === 'enable' ? 'password' : 'confirm'
  notice.value = ''
}

async function refresh() {
  await Promise.all([client.invalidateQueries({ queryKey: keys.totp }), auth.load(true)])
}

async function submitPassword() {
  form.clear()
  if (!password.value) {
    form.errors.current_password = t('totp.enterPassword')
    return
  }
  const ok = await form.submit(async () => {
    setup.value = await totpApi.setup(password.value)
  })
  if (ok) {
    password.value = ''
    step.value = 'scan'
  }
}

async function submitFirstCode() {
  form.clear()
  if (!code.value.trim()) {
    form.errors.code = t('totp.enterCode')
    return
  }
  const ok = await form.submit(async () => {
    codes.value = (await totpApi.enable(code.value)).recovery_codes
  })
  if (ok) {
    code.value = ''
    setup.value = null // секрет после подключения больше не нужен на экране
    step.value = 'codes'
    notice.value = t('totp.enabledNotice')
    await refresh()
  }
}

async function submitConfirm() {
  form.clear()
  if (!password.value) form.errors.current_password = t('totp.enterPassword')
  if (!code.value.trim()) form.errors.code = t('totp.enterCodeOrRecovery')
  if (Object.keys(form.errors).length) return
  const current = mode.value
  const ok = await form.submit(async () => {
    if (current === 'disable') await totpApi.disable(password.value, code.value)
    else codes.value = (await totpApi.renewRecoveryCodes(password.value, code.value)).recovery_codes
  })
  if (!ok) return
  password.value = ''
  code.value = ''
  if (current === 'disable') {
    notice.value = t('totp.disabledNotice')
    reset()
  } else {
    step.value = 'codes'
    notice.value = t('totp.renewedNotice')
  }
  await refresh()
}

const title = computed(() => (mode.value === 'enable' ? t('totp.titleEnable') : mode.value === 'renew' ? t('totp.titleRenew') : t('totp.titleDisable')))
const lowCodes = computed(() => (status.data.value?.enabled ? status.data.value.recovery_left <= 2 : false))
</script>

<template>
  <div class="totp" data-testid="totp">
    <UiSkeleton v-if="status.isPending.value" :lines="2" :label="$t('totp.loading')" />
    <UiAlert v-else-if="status.isError.value" tone="danger">
      {{ $t('totp.statusFailed') }}
      <UiButton variant="link" @click="status.refetch()">{{ $t('totp.retry') }}</UiButton>
    </UiAlert>

    <template v-else-if="status.data.value?.enabled">
      <p class="state state--on" data-testid="totp-state">{{ $t('totp.on') }}</p>
      <p :class="{ warn: lowCodes }" data-testid="totp-left">
        <i18n-t keypath="totp.left" scope="global"><template #n><strong>{{ status.data.value.recovery_left }}</strong></template></i18n-t>
        <template v-if="lowCodes"> {{ $t('totp.lowLeft') }}</template>
      </p>
      <div class="actions">
        <UiButton icon="refresh" data-testid="totp-renew" @click="start('renew')">{{ $t('totp.renew') }}</UiButton>
        <UiButton variant="ghost" data-testid="totp-disable" @click="start('disable')">{{ $t('totp.disable') }}</UiButton>
      </div>
    </template>

    <template v-else>
      <p class="state" data-testid="totp-state">{{ $t('totp.off') }}</p>
      <UiButton icon="shield" data-testid="totp-enable" @click="start('enable')">{{ $t('totp.enable') }}</UiButton>
    </template>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice && !open" class="notice" data-testid="totp-notice">{{ notice }}</p>

    <UiModal v-model:open="open" :title="title" size="md" testid="totp-dialog">
      <!-- 1. пароль -->
      <form v-if="mode === 'enable' && step === 'password'" novalidate @submit.prevent="submitPassword">
        <p>{{ $t('totp.step1') }}</p>
        <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
        <UiField id="totp-password" :label="$t('common.password')" :error="form.errors.current_password">
          <UiInput v-model="password" type="password" autocomplete="current-password" reveal />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="totp-next">{{ $t('totp.next') }}</UiButton>
          <UiButton variant="link" @click="open = false">{{ $t('totp.cancel') }}</UiButton>
        </div>
      </form>

      <!-- 2. QR и первый код -->
      <form v-else-if="mode === 'enable' && step === 'scan' && setup" novalidate @submit.prevent="submitFirstCode">
        <ol class="how">
          <li>{{ $t('totp.scan1') }}</li>
          <li>{{ $t('totp.scan2') }}</li>
        </ol>
        <div class="scan">
          <QrCode :value="setup.uri" :label="$t('totp.qr')" />
          <div class="scan__manual">
            <p class="scan__label">{{ $t('totp.secretLabel') }}</p>
            <p class="scan__secret" data-testid="totp-secret">{{ groupSecret(setup.secret) }}</p>
            <p class="scan__hint">{{ $t('totp.secretHint') }}</p>
          </div>
        </div>
        <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
        <UiField id="totp-code" :label="$t('totp.codeLabel')" :hint="$t('totp.codeHint')" :error="form.errors.code">
          <UiInput v-model="code" inputmode="numeric" autocomplete="one-time-code" :maxlength="12" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="totp-confirm">{{ $t('totp.enableSubmit') }}</UiButton>
          <UiButton variant="link" @click="open = false">{{ $t('totp.cancel') }}</UiButton>
        </div>
      </form>

      <!-- пароль и код: новые одноразовые коды или выключение -->
      <form v-else-if="step === 'confirm'" novalidate @submit.prevent="submitConfirm">
        <p v-if="mode === 'disable'">{{ $t('totp.confirmDisable') }}</p>
        <p v-else>{{ $t('totp.confirmRenew') }}</p>
        <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
        <UiField id="totp-password" :label="$t('common.password')" :error="form.errors.current_password">
          <UiInput v-model="password" type="password" autocomplete="current-password" reveal />
        </UiField>
        <UiField id="totp-code" :label="$t('totp.codeOrRecoveryLabel')" :error="form.errors.code">
          <UiInput v-model="code" autocomplete="one-time-code" :maxlength="16" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" :variant="mode === 'disable' ? 'danger' : 'primary'" :loading="form.submitting.value" data-testid="totp-confirm">
            {{ mode === 'disable' ? $t('totp.disableSubmit') : $t('totp.renewSubmit') }}
          </UiButton>
          <UiButton variant="link" @click="open = false">{{ $t('totp.cancel') }}</UiButton>
        </div>
      </form>

      <!-- 3. одноразовые коды -->
      <div v-else-if="step === 'codes'">
        <p>
          <i18n-t keypath="totp.codesIntro" scope="global">
            <template #once><strong>{{ $t('totp.onceWord') }}</strong></template>
            <template #now><strong>{{ $t('totp.nowWord') }}</strong></template>
          </i18n-t>
        </p>
        <SecretCodesBox
          :codes="codes"
          :what="$t('totp.codesWhat')"
          :login="auth.user?.login ?? ''"
          filename="kupol-one-time-codes.txt"
          :note="$t('totp.codesNote')"
        />
        <div class="confirm">
          <UiCheckbox v-model="saved" :label="$t('common.savedCodesPlural')" />
        </div>
        <UiAlert v-if="closeHint && !saved" tone="warning" data-testid="totp-close-hint">{{ closeHint }}</UiAlert>
        <div class="dlg-actions">
          <UiButton variant="primary" :disabled="!saved" icon-end="check" data-testid="totp-done" @click="open = false">{{ $t('totp.done') }}</UiButton>
        </div>
      </div>
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
.warn {
  color: var(--red-800);
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
.how {
  margin: 0 0 var(--space-4);
  padding-left: var(--space-5);
}
.scan {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
  align-items: flex-start;
  margin-bottom: var(--space-4);
}
.scan__manual {
  flex: 1 1 12rem;
  min-width: 0;
}
.scan__label {
  margin: 0 0 var(--space-1);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.scan__secret {
  margin: 0 0 var(--space-2);
  font-family: var(--font-doc);
  font-size: var(--text-lg);
  font-weight: 700;
  overflow-wrap: anywhere;
  user-select: all;
}
.scan__hint {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.confirm {
  margin: var(--space-4) 0 var(--space-3);
}
</style>
