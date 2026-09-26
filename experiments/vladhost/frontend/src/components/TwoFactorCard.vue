<script setup lang="ts">
// Двухфакторный вход (TOTP): включение по QR-коду, коды восстановления, выключение.
import { CopyOutline, KeyOutline, LockClosedOutline, ShieldCheckmarkOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, useMessage } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { api, apiVoid, ApiError } from '@/api/client'
import { recoveryCodesSchema, twoFactorSetupSchema, twoFactorStatusSchema, type TwoFactorStatus } from '@/api/schemas'
import QrCode from '@/components/QrCode.vue'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const auth = useAuthStore()
const message = useMessage()

// idle — обзор; password — пароль перед выдачей ключа; scan — QR и код; codes — показать коды восстановления;
// regen — пароль для новых кодов; disable — пароль и код для выключения.
type Mode = 'idle' | 'password' | 'scan' | 'codes' | 'regen' | 'disable'
const mode = ref<Mode>('idle')
const status = ref<TwoFactorStatus | null>(null)
const busy = ref(false)
const error = ref('')
const password = ref('')
const code = ref('')
const setup = ref<{ secret: string; uri: string } | null>(null)
const codes = ref<string[]>([])

// Ключ группами по 4 символа — так его проще перепечатать вручную.
const secretGroups = computed(() => setup.value?.secret.match(/.{1,4}/g)?.join(' ') ?? '')

async function load() {
  try {
    status.value = await api('/api/me/2fa', { schema: twoFactorStatusSchema })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('settings.twoFactor.loadFailed')
  }
}
onMounted(load)

function go(m: Mode) {
  mode.value = m
  error.value = ''
  password.value = ''
  code.value = ''
}

async function run(fn: () => Promise<void>) {
  error.value = ''
  busy.value = true
  try {
    await fn()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('settings.twoFactor.failed')
  } finally {
    busy.value = false
  }
}

const start = () =>
  run(async () => {
    setup.value = await api('/api/me/2fa/setup', { method: 'POST', body: { password: password.value }, schema: twoFactorSetupSchema })
    go('scan')
  })

const enable = () =>
  run(async () => {
    const r = await api('/api/me/2fa/enable', { method: 'POST', body: { code: code.value }, schema: recoveryCodesSchema })
    codes.value = r.recovery_codes
    setup.value = null
    go('codes')
    message.success(t('settings.twoFactor.enabled'))
    await Promise.all([load(), auth.refreshMe()])
  })

const regenerate = () =>
  run(async () => {
    const r = await api('/api/me/2fa/recovery', { method: 'POST', body: { password: password.value }, schema: recoveryCodesSchema })
    codes.value = r.recovery_codes
    go('codes')
    await load()
  })

const disable = () =>
  run(async () => {
    await apiVoid('/api/me/2fa/disable', { method: 'POST', body: { password: password.value, code: code.value } })
    go('idle')
    message.success(t('settings.twoFactor.disabled'))
    await Promise.all([load(), auth.refreshMe()])
  })

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}

function finishCodes() {
  codes.value = []
  go('idle')
}
</script>

<template>
  <section class="tfa glass rise" style="--i: 5">
    <div class="head">
      <div>
        <h3>{{ t('settings.twoFactor.title') }}</h3>
        <p class="hint">{{ t('settings.twoFactor.hint') }}</p>
      </div>
      <status-chip v-if="status" :tone="status.enabled ? 'emerald' : 'slate'">
        {{
          status.enabled && status.enabled_at
            ? t('settings.twoFactor.onSince', { date: formatDateTime(status.enabled_at, locale) })
            : t('settings.twoFactor.off')
        }}
      </status-chip>
    </div>

    <n-alert v-if="error" type="error" :show-icon="false" class="gap">{{ error }}</n-alert>

    <!-- обзор -->
    <template v-if="mode === 'idle' && status">
      <template v-if="status.enabled">
        <p class="hint" :class="{ warn: status.recovery_left <= 3 }">
          {{ t('settings.twoFactor.recoveryLeft', { n: status.recovery_left }) }}
        </p>
        <div class="actions">
          <n-button @click="go('regen')">{{ t('settings.twoFactor.regenerate') }}</n-button>
          <n-button class="tint-rose" @click="go('disable')">{{ t('settings.twoFactor.disable') }}</n-button>
        </div>
      </template>
      <div v-else class="actions">
        <n-button type="primary" @click="go('password')">
          <template #icon><n-icon :component="ShieldCheckmarkOutline" /></template>
          {{ t('settings.twoFactor.enable') }}
        </n-button>
      </div>
    </template>

    <!-- пароль: перед выдачей ключа или новых кодов -->
    <n-form v-else-if="mode === 'password' || mode === 'regen'" class="form" @submit.prevent="mode === 'password' ? start() : regenerate()">
      <p class="hint">{{ mode === 'password' ? t('settings.twoFactor.passwordHint') : t('settings.twoFactor.regenerateHint') }}</p>
      <n-form-item :label="t('settings.twoFactor.password')">
        <n-input v-model:value="password" type="password" show-password-on="click" autocomplete="current-password" :input-props="{ 'aria-label': t('settings.twoFactor.password') }">
          <template #prefix><n-icon :component="LockClosedOutline" /></template>
        </n-input>
      </n-form-item>
      <div class="actions">
        <n-button type="primary" attr-type="submit" :loading="busy" :disabled="!password">{{ t('settings.twoFactor.continue') }}</n-button>
        <n-button quaternary @click="go('idle')">{{ t('common.cancel') }}</n-button>
      </div>
    </n-form>

    <!-- QR и первый код -->
    <div v-else-if="mode === 'scan' && setup" class="scan">
      <qr-code :value="setup.uri" :label="t('settings.twoFactor.qrLabel')" />
      <n-form class="form" @submit.prevent="enable">
        <ol class="steps">
          <li>{{ t('settings.twoFactor.step1') }}</li>
          <li>{{ t('settings.twoFactor.step2') }}</li>
        </ol>
        <p class="hint">{{ t('settings.twoFactor.manual') }}</p>
        <div class="secret">
          <code>{{ secretGroups }}</code>
          <n-button size="small" quaternary :aria-label="t('settings.twoFactor.copySecret')" @click="copy(setup.secret)">
            <template #icon><n-icon :component="CopyOutline" /></template>
          </n-button>
        </div>
        <n-form-item :label="t('settings.twoFactor.code')">
          <n-input
            v-model:value="code"
            placeholder="123 456"
            autocomplete="one-time-code"
            :maxlength="7"
            :input-props="{ 'aria-label': t('settings.twoFactor.code'), inputmode: 'numeric' }"
          >
            <template #prefix><n-icon :component="KeyOutline" /></template>
          </n-input>
        </n-form-item>
        <div class="actions">
          <n-button type="primary" attr-type="submit" :loading="busy" :disabled="!code">{{ t('settings.twoFactor.confirm') }}</n-button>
          <n-button quaternary @click="go('idle')">{{ t('common.cancel') }}</n-button>
        </div>
      </n-form>
    </div>

    <!-- коды восстановления: показываются один раз -->
    <div v-else-if="mode === 'codes'" class="codes-box">
      <n-alert type="warning" :show-icon="false">{{ t('settings.twoFactor.codesWarning') }}</n-alert>
      <ul class="codes">
        <li v-for="c in codes" :key="c"><code>{{ c }}</code></li>
      </ul>
      <div class="actions">
        <n-button @click="copy(codes.join('\n'))">
          <template #icon><n-icon :component="CopyOutline" /></template>
          {{ t('settings.twoFactor.copyCodes') }}
        </n-button>
        <n-button type="primary" @click="finishCodes">{{ t('settings.twoFactor.saved') }}</n-button>
      </div>
    </div>

    <!-- выключение -->
    <n-form v-else-if="mode === 'disable'" class="form" @submit.prevent="disable">
      <p class="hint">{{ t('settings.twoFactor.disableHint') }}</p>
      <n-form-item :label="t('settings.twoFactor.password')">
        <n-input v-model:value="password" type="password" show-password-on="click" autocomplete="current-password" :input-props="{ 'aria-label': t('settings.twoFactor.password') }">
          <template #prefix><n-icon :component="LockClosedOutline" /></template>
        </n-input>
      </n-form-item>
      <n-form-item :label="t('settings.twoFactor.codeOrRecovery')">
        <n-input v-model:value="code" autocomplete="one-time-code" :maxlength="16" :input-props="{ 'aria-label': t('settings.twoFactor.codeOrRecovery') }">
          <template #prefix><n-icon :component="KeyOutline" /></template>
        </n-input>
      </n-form-item>
      <div class="actions">
        <n-button class="tint-rose" attr-type="submit" :loading="busy" :disabled="!password || !code">{{ t('settings.twoFactor.disable') }}</n-button>
        <n-button quaternary @click="go('idle')">{{ t('common.cancel') }}</n-button>
      </div>
    </n-form>
  </section>
</template>

<style scoped>
.tfa {
  padding: 22px 26px;
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

h3 {
  font-size: 17px;
  margin-bottom: 8px;
}

.hint {
  margin: 0 0 12px;
  font-size: 13.5px;
  color: var(--text-dim);
}

.hint.warn {
  color: var(--amber);
}

.gap {
  margin: 12px 0;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}

.form {
  max-width: 420px;
  margin-top: 12px;
}

.scan {
  display: flex;
  flex-wrap: wrap;
  gap: 26px;
  align-items: flex-start;
  margin-top: 14px;
}

.scan .form {
  margin-top: 0;
  flex: 1 1 280px;
}

.steps {
  margin: 0 0 10px;
  padding-left: 20px;
  display: grid;
  gap: 6px;
}

.secret {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 14px;
}

.secret code {
  font-size: 14px;
  letter-spacing: 0.04em;
  word-break: break-all;
}

.codes-box {
  margin-top: 14px;
  display: grid;
  gap: 14px;
  max-width: 520px;
}

.codes {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 8px 16px;
}

.codes code {
  font-size: 15px;
  letter-spacing: 0.05em;
}
</style>
