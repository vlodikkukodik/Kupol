<script setup lang="ts">
import { AddOutline, CopyOutline, KeyOutline, PeopleOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NSpace, NSwitch, useMessage } from 'naive-ui'
import { computed, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import {
  fieldErrors,
  ftpAccountForm,
  ftpAccountGrantSchema,
  ftpAccountResponseSchema,
  ftpGrantSchema,
  siteResponseSchema,
  type FtpAccount,
  type Site,
} from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()
const message = useMessage()
const busy = ref(false)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

// Выданный FTP-пароль: показывается один раз, на сервере остаётся только хеш.
const grant = ref<{ username: string; password: string } | null>(null)

async function enable() {
  busy.value = true
  try {
    const r = await api(`/api/sites/${props.site.id}/ftp`, { method: 'POST', schema: ftpGrantSchema })
    grant.value = { username: r.site.ftp.username ?? '', password: r.password }
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.ftp.enableFailed')))
  } finally {
    busy.value = false
  }
}

async function disable() {
  busy.value = true
  try {
    await api(`/api/sites/${props.site.id}/ftp`, { method: 'DELETE', schema: siteResponseSchema })
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.ftp.disableFailed')))
  } finally {
    busy.value = false
  }
}

// --- дополнительные аккаунты ---
const accounts = computed(() => props.site.ftp.accounts)
const atLimit = computed(() => accounts.value.length >= props.site.ftp.accounts_limit)
const mainLogin = computed(() => props.site.ftp.username ?? '')

const form = reactive({ name: '', dir: '', readOnly: false })
const formErrors = ref<Record<string, string>>({})
const creating = ref(false)
const loginPreview = computed(() => `${form.name.trim().toLowerCase() || 'name'}.${mainLogin.value}`)
const busyAcct = ref<number | null>(null)
const base = computed(() => `/api/sites/${props.site.id}/ftp/accounts`)

async function createAccount() {
  const parsed = ftpAccountForm.safeParse({ name: form.name })
  formErrors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  creating.value = true
  try {
    const r = await api(base.value, {
      method: 'POST',
      body: { name: parsed.data.name, dir: form.dir.trim(), read_only: form.readOnly },
      schema: ftpAccountGrantSchema,
    })
    grant.value = { username: r.account.username, password: r.password }
    form.name = ''
    form.dir = ''
    form.readOnly = false
    message.success(t('ftpAccounts.created'))
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    if (e instanceof ApiError && e.field) formErrors.value = { [e.field === 'dir' ? 'dir' : 'name']: e.message }
    else message.error(errText(e, t('ftpAccounts.createFailed')))
  } finally {
    creating.value = false
  }
}

async function patchAccount(a: FtpAccount, body: Record<string, unknown>): Promise<boolean> {
  busyAcct.value = a.id
  try {
    await api(`${base.value}/${a.id}`, { method: 'PATCH', body, schema: ftpAccountResponseSchema })
    await store.load(t('sites.loadFailed'))
    return true
  } catch (e) {
    message.error(errText(e, t('ftpAccounts.updateFailed')))
    return false
  } finally {
    busyAcct.value = null
  }
}

async function resetPassword(a: FtpAccount) {
  busyAcct.value = a.id
  try {
    const r = await api(`${base.value}/${a.id}/password`, { method: 'POST', schema: ftpAccountGrantSchema })
    grant.value = { username: r.account.username, password: r.password }
  } catch (e) {
    message.error(errText(e, t('ftpAccounts.passwordFailed')))
  } finally {
    busyAcct.value = null
  }
}

async function removeAccount(a: FtpAccount) {
  busyAcct.value = a.id
  try {
    await api(`${base.value}/${a.id}`, { method: 'DELETE' })
    message.success(t('ftpAccounts.removed'))
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('ftpAccounts.removeFailed')))
  } finally {
    busyAcct.value = null
  }
}

// Правка папки и режима в отдельном окне.
const editing = ref<FtpAccount | null>(null)
const edit = reactive({ dir: '', readOnly: false })
const editError = ref('')
const saving = ref(false)

function openEdit(a: FtpAccount) {
  editing.value = a
  edit.dir = a.dir
  edit.readOnly = a.read_only
  editError.value = ''
}

async function saveEdit() {
  const a = editing.value
  if (!a) return
  saving.value = true
  editError.value = ''
  try {
    await api(`${base.value}/${a.id}`, {
      method: 'PATCH',
      body: { dir: edit.dir.trim(), read_only: edit.readOnly },
      schema: ftpAccountResponseSchema,
    })
    editing.value = null
    message.success(t('ftpAccounts.updated'))
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    editError.value = errText(e, t('ftpAccounts.updateFailed'))
  } finally {
    saving.value = false
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('sites.ftp.title') }}</h1>
    </header>

    <n-alert v-if="!site.ftp.available" type="info" :show-icon="false">{{ t('siteArea.ftpUnavailable') }}</n-alert>

    <section v-else class="glass ftp rise" style="--i: 1">
      <span class="ftp-ic"><n-icon :size="18" :component="KeyOutline" /></span>
      <div class="ftp-body">
        <strong>{{ t('sites.ftp.title') }}</strong>
        <span v-if="site.ftp.enabled" class="ftp-line">
          {{ t('sites.ftp.connection', { host: site.ftp.host ?? '', port: site.ftp.port ?? 0, user: site.ftp.username ?? '' }) }}
        </span>
      </div>
      <n-space v-if="site.ftp.enabled" :size="8">
        <n-popconfirm @positive-click="enable">
          <template #trigger>
            <n-button size="small" class="tint-violet" :disabled="busy">{{ t('sites.ftp.newPassword') }}</n-button>
          </template>
          {{ t('sites.ftp.newPasswordConfirm') }}
        </n-popconfirm>
        <n-button size="small" class="tint-rose" :disabled="busy" @click="disable">{{ t('sites.ftp.disable') }}</n-button>
      </n-space>
      <n-button v-else size="small" class="tint-violet" :loading="busy" @click="enable">{{ t('sites.ftp.enable') }}</n-button>
    </section>

    <template v-if="site.ftp.available">
      <section class="glass card rise" style="--i: 2">
        <div class="card-head">
          <span class="ftp-ic team"><n-icon :size="18" :component="PeopleOutline" /></span>
          <div>
            <h3>{{ t('ftpAccounts.title') }}</h3>
            <p class="note">{{ t('ftpAccounts.hint') }}</p>
          </div>
        </div>

        <ul v-if="accounts.length" class="acc-list">
          <li v-for="a in accounts" :key="a.id" class="acc" :class="{ off: !a.enabled }">
            <div class="acc-main">
              <code class="acc-login">{{ a.username }}</code>
              <n-button size="tiny" class="tint-violet" @click="copyText(a.username)">
                <template #icon><n-icon :component="CopyOutline" /></template>
                {{ t('common.copy') }}
              </n-button>
              <status-chip tone="cyan">{{ a.dir ? t('ftpAccounts.scopeDir', { dir: a.dir }) : t('ftpAccounts.scopeSite') }}</status-chip>
              <status-chip :tone="a.read_only ? 'amber' : 'emerald'">{{ a.read_only ? t('ftpAccounts.modeRead') : t('ftpAccounts.modeWrite') }}</status-chip>
              <status-chip v-if="!a.enabled" tone="slate">{{ t('ftpAccounts.disabled') }}</status-chip>
            </div>
            <div class="acc-foot">
              <span class="note">
                {{ a.last_login_at ? t('ftpAccounts.lastLogin', { date: formatDateTime(a.last_login_at, locale) }) : t('ftpAccounts.neverLogin') }}
              </span>
              <span class="grow" />
              <label class="switch">
                <n-switch
                  size="small"
                  :value="a.enabled"
                  :disabled="busyAcct === a.id"
                  :aria-label="`${t('ftpAccounts.enabled')}: ${a.username}`"
                  @update:value="(v: boolean) => patchAccount(a, { enabled: v })"
                />
                {{ t('ftpAccounts.enabled') }}
              </label>
              <n-button size="small" class="tint-cyan" :disabled="busyAcct === a.id" @click="openEdit(a)">{{ t('ftpAccounts.edit') }}</n-button>
              <n-popconfirm @positive-click="resetPassword(a)">
                <template #trigger>
                  <n-button size="small" class="tint-violet" :disabled="busyAcct === a.id">{{ t('ftpAccounts.newPassword') }}</n-button>
                </template>
                {{ t('ftpAccounts.newPasswordConfirm') }}
              </n-popconfirm>
              <n-popconfirm @positive-click="removeAccount(a)">
                <template #trigger>
                  <n-button size="small" class="tint-rose" :disabled="busyAcct === a.id">{{ t('ftpAccounts.remove') }}</n-button>
                </template>
                {{ t('ftpAccounts.removeConfirm', { login: a.username }) }}
              </n-popconfirm>
            </div>
          </li>
        </ul>
        <p v-else class="note">{{ t('ftpAccounts.empty') }}</p>

        <n-alert v-if="atLimit" type="info" :show-icon="false" class="limit">
          {{ t('ftpAccounts.limit', { max: site.ftp.accounts_limit }) }}
        </n-alert>
        <n-form v-else class="acc-form" @submit.prevent="createAccount">
          <n-form-item
            :label="t('ftpAccounts.name')"
            :validation-status="formErrors.name ? 'error' : undefined"
            :feedback="formErrors.name ? resolveMessage(formErrors.name) : t('ftpAccounts.loginPreview', { login: loginPreview })"
          >
            <n-input v-model:value="form.name" :placeholder="t('ftpAccounts.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('ftpAccounts.name') }" @update:value="formErrors.name = ''" />
          </n-form-item>
          <n-form-item
            :label="t('ftpAccounts.dir')"
            :validation-status="formErrors.dir ? 'error' : undefined"
            :feedback="formErrors.dir ? resolveMessage(formErrors.dir) : t('ftpAccounts.dirHint')"
          >
            <n-input v-model:value="form.dir" :placeholder="t('ftpAccounts.dirPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('ftpAccounts.dir') }" @update:value="formErrors.dir = ''" />
          </n-form-item>
          <label class="switch ro">
            <n-switch v-model:value="form.readOnly" :aria-label="t('ftpAccounts.readOnly')" />
            <span><strong>{{ t('ftpAccounts.readOnly') }}</strong><small>{{ t('ftpAccounts.readOnlyHint') }}</small></span>
          </label>
          <n-button type="primary" attr-type="submit" :loading="creating">
            <template #icon><n-icon :component="AddOutline" /></template>
            {{ t('ftpAccounts.create') }}
          </n-button>
        </n-form>
      </section>
    </template>

    <n-modal :show="editing !== null" preset="card" :title="t('ftpAccounts.editTitle', { login: editing?.username ?? '' })" style="max-width: 460px" @update:show="editing = null">
      <n-form @submit.prevent="saveEdit">
        <n-form-item
          :label="t('ftpAccounts.dir')"
          :validation-status="editError ? 'error' : undefined"
          :feedback="editError ? resolveMessage(editError) : t('ftpAccounts.dirHint')"
        >
          <n-input v-model:value="edit.dir" :placeholder="t('ftpAccounts.dirPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('ftpAccounts.dir') }" @update:value="editError = ''" />
        </n-form-item>
        <label class="switch ro">
          <n-switch v-model:value="edit.readOnly" :aria-label="t('ftpAccounts.readOnly')" />
          <span><strong>{{ t('ftpAccounts.readOnly') }}</strong><small>{{ t('ftpAccounts.readOnlyHint') }}</small></span>
        </label>
        <n-button type="primary" attr-type="submit" :loading="saving">{{ t('ftpAccounts.save') }}</n-button>
      </n-form>
    </n-modal>

    <n-modal :show="grant !== null" preset="card" :title="t('sites.ftp.dialogTitle')" style="max-width: 480px" @update:show="grant = null">
      <template v-if="grant">
        <p class="note">{{ t('sites.ftp.once') }}</p>
        <dl class="creds">
          <dt>{{ t('sites.ftp.server') }}</dt>
          <dd><code>{{ site.ftp.host }}</code></dd>
          <dt>{{ t('sites.ftp.port') }}</dt>
          <dd><code>{{ site.ftp.port }}</code></dd>
          <dt>{{ t('sites.ftp.login') }}</dt>
          <dd>
            <code>{{ grant.username }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.username)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('sites.ftp.password') }}</dt>
          <dd>
            <code data-testid="ftp-password" class="pw">{{ grant.password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
        </dl>
        <p class="note">{{ t(site.ftp.allow_plain ? 'sites.ftp.howTo' : 'sites.ftp.howToSecure') }}</p>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 900px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.ftp {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  padding: 18px 22px;
}

.ftp-ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-violet);
}

.ftp-body {
  flex: 1;
  min-width: 180px;
  display: grid;
}

.ftp-line,
.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.card {
  padding: 18px 22px 20px;
}

.card-head {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  margin-bottom: 14px;
}

.card-head h3 {
  font-size: 16px;
}

.card-head .note {
  margin: 2px 0 0;
}

.ftp-ic.team {
  background: var(--grad-cyan);
  flex: none;
}

.acc-list {
  list-style: none;
  margin: 0 0 16px;
  padding: 0;
  display: grid;
  gap: 10px;
}

.acc {
  padding: 12px 14px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.acc.off {
  opacity: 0.7;
}

.acc-main,
.acc-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.acc-foot {
  margin-top: 10px;
}

.acc-login {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.grow {
  flex: 1;
}

.switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--text-dim);
  font-size: 13.5px;
  cursor: pointer;
}

.switch.ro {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin: 4px 0 16px;
}

.switch.ro span {
  display: grid;
  color: var(--text);
}

.switch.ro small {
  color: var(--text-dim);
  font-size: 12.5px;
}

.acc-form {
  max-width: 460px;
}

.limit {
  margin-top: 4px;
}

.creds {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 10px 16px;
  margin: 14px 0;
  padding: 14px 16px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.creds dt {
  color: var(--text-faint);
}

.creds dd {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  word-break: break-all;
}

.pw {
  font-size: 15px;
  letter-spacing: 0.04em;
  color: #fde68a;
  background: rgba(251, 191, 36, 0.1);
  border-color: rgba(251, 191, 36, 0.35);
}
</style>
