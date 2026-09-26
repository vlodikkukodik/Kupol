<script setup lang="ts">
import { AddOutline, CheckmarkCircleOutline, CopyOutline, MailOutline, RefreshOutline, WarningOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NCheckbox, NDatePicker, NForm, NFormItem, NIcon, NInput, NInputNumber, NModal, NPopconfirm, NSelect, NSpace, NSwitch, NTabPane, NTabs, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import {
  fieldErrors,
  mailAliasForm,
  mailAutoReplyForm,
  mailboxCreatedSchema,
  mailboxForm,
  mailJournalSchema,
  type MailJournal,
  mailDomainForm,
  mailDomainResponseSchema,
  mailOverviewSchema,
  mailPasswordSchema,
  mailRecordsSchema,
  type MailAuto,
  type MailAlias,
  type Mailbox,
  type MailDomain,
  type MailRecord,
} from '@/api/schemas'
import { resolveMessage, useI18n } from '@/i18n'

const { t } = useI18n()
const message = useMessage()

type Overview = ReturnType<typeof mailOverviewSchema.parse>
const overview = ref<Overview | null>(null)
const loadError = ref('')
const info = computed(() => overview.value?.info)
const domains = computed<MailDomain[]>(() => overview.value?.domains ?? [])
const eligibleOptions = computed(() => (info.value?.eligible_domains ?? []).map((d) => ({ label: d, value: d })))
const boxCount = computed(() => domains.value.reduce((n, d) => n + d.mailboxes.length, 0))
const aliasCount = computed(() => domains.value.reduce((n, d) => n + d.aliases.length, 0))

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

// Записи DNS проверяются отдельными запросами: проверка обращается к чужим серверам и может быть медленной.
const records = reactive<Record<number, MailRecord[]>>({})
const checking = reactive<Record<number, boolean>>({})
const autos = reactive<Record<number, MailAuto>>({})
const autoBusy = reactive<Record<number, boolean>>({})

async function checkDNS(d: MailDomain) {
  checking[d.id] = true
  try {
    const r = await api(`/api/mail/domains/${d.id}/dns`, { schema: mailRecordsSchema })
    records[d.id] = r.records
    autos[d.id] = r.auto
  } catch (e) {
    message.error(errText(e, t('mailhost.dnsFailed')))
  } finally {
    checking[d.id] = false
  }
}

async function load() {
  try {
    overview.value = await api('/api/mail', { schema: mailOverviewSchema })
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('mailhost.loadFailed'))
    return
  }
  for (const d of domains.value) if (!records[d.id]) void checkDNS(d)
}
onMounted(load)

// Домен на наших серверах имён: записи почты ставятся в его зону одной кнопкой.
async function autoConfigure(d: MailDomain) {
  autoBusy[d.id] = true
  try {
    const r = await api(`/api/mail/domains/${d.id}/dns/auto`, { method: 'POST', schema: mailRecordsSchema })
    records[d.id] = r.records
    autos[d.id] = r.auto
    message.success(t('mailhost.auto.done'))
  } catch (e) {
    message.error(errText(e, t('mailhost.auto.failed')))
  } finally {
    autoBusy[d.id] = false
  }
}

// --- домены ---
const domainForm = reactive({ domain: null as string | null })
const domainErrors = ref<Record<string, string>>({})
const enabling = ref(false)

async function enableDomain() {
  const parsed = mailDomainForm.safeParse({ domain: domainForm.domain ?? '' })
  domainErrors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  enabling.value = true
  try {
    await api('/api/mail/domains', { method: 'POST', body: parsed.data, schema: mailDomainResponseSchema })
    domainForm.domain = null
    message.success(t('mailhost.domainEnabled'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) domainErrors.value = { [e.field]: e.message }
    else message.error(errText(e, t('mailhost.enableFailed')))
  } finally {
    enabling.value = false
  }
}

async function toggleDomain(d: MailDomain) {
  try {
    await api(`/api/mail/domains/${d.id}`, { method: 'PATCH', body: { enabled: !d.enabled } })
    await load()
  } catch (e) {
    message.error(errText(e, t('mailhost.toggleFailed')))
  }
}

async function removeDomain(d: MailDomain) {
  try {
    await api(`/api/mail/domains/${d.id}`, { method: 'DELETE' })
    delete records[d.id]
    message.success(t('mailhost.domainRemoved'))
    await load()
  } catch (e) {
    message.error(errText(e, t('mailhost.removeFailed')))
  }
}

// --- ящики ---
const boxForms = reactive<Record<number, { local: string; password: string; quota: number | null }>>({})
const boxErrors = reactive<Record<number, Record<string, string>>>({})
const creating = reactive<Record<number, boolean>>({})
const shownPassword = ref<{ address: string; password: string } | null>(null)

function boxForm(d: MailDomain) {
  return (boxForms[d.id] ??= { local: '', password: '', quota: info.value?.default_quota_mb ?? 500 })
}

async function createMailbox(d: MailDomain) {
  const f = boxForm(d)
  const parsed = mailboxForm.safeParse({ local: f.local, password: f.password, quota_mb: f.quota ?? Number.NaN })
  boxErrors[d.id] = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  creating[d.id] = true
  try {
    const r = await api(`/api/mail/domains/${d.id}/mailboxes`, { method: 'POST', body: parsed.data, schema: mailboxCreatedSchema })
    if (r.password) shownPassword.value = { address: `${r.mailbox.local}@${d.domain}`, password: r.password }
    f.local = ''
    f.password = ''
    message.success(t('mailhost.mailboxCreated'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) boxErrors[d.id] = { [e.field]: e.message }
    else message.error(errText(e, t('mailhost.mailboxFailed')))
  } finally {
    creating[d.id] = false
  }
}

async function toggleMailbox(b: Mailbox) {
  try {
    await api(`/api/mail/mailboxes/${b.id}`, { method: 'PATCH', body: { enabled: !b.enabled } })
    await load()
  } catch (e) {
    message.error(errText(e, t('mailhost.mailboxToggleFailed')))
  }
}

async function removeMailbox(b: Mailbox) {
  try {
    await api(`/api/mail/mailboxes/${b.id}`, { method: 'DELETE' })
    message.success(t('mailhost.mailboxRemoved'))
    await load()
  } catch (e) {
    message.error(errText(e, t('mailhost.removeFailed')))
  }
}

// Размер ящика и пароль меняются в одном окне.
const editing = ref<{ box: Mailbox; address: string; quota: number | null; password: string; error: string } | null>(null)
const saving = ref(false)
const tab = ref('main')

// Правила ящика: автоответчик и пересылка.
const rules = reactive({
  replyOn: false,
  subject: '',
  body: '',
  from: null as string | null,
  to: null as string | null,
  days: 1 as number | null,
  forwardTo: '',
  keepCopy: true,
})
const ruleErrors = ref<Record<string, string>>({})

function edit(d: MailDomain, b: Mailbox) {
  editing.value = { box: b, address: `${b.local}@${d.domain}`, quota: b.quota_mb, password: '', error: '' }
  tab.value = 'main'
  const ar = b.autoreply
  Object.assign(rules, {
    replyOn: ar.enabled,
    subject: ar.subject,
    body: ar.body,
    from: ar.from || null,
    to: ar.to || null,
    days: ar.days || 1,
    forwardTo: b.forward.to.join(', '),
    keepCopy: b.forward.keep_copy,
  })
  ruleErrors.value = {}
}

async function saveRules() {
  const e = editing.value
  if (!e) return
  ruleErrors.value = {}
  let reply: Record<string, unknown> = { enabled: false }
  if (rules.replyOn) {
    const parsed = mailAutoReplyForm.safeParse({ subject: rules.subject, body: rules.body, days: rules.days ?? Number.NaN })
    if (!parsed.success) {
      ruleErrors.value = fieldErrors(parsed.error)
      tab.value = 'reply'
      return
    }
    reply = { enabled: true, ...parsed.data, from: rules.from ?? '', to: rules.to ?? '' }
  }
  const to = rules.forwardTo.split(/[\s,;]+/).filter(Boolean)
  saving.value = true
  try {
    await api(`/api/mail/mailboxes/${e.box.id}/rules`, { method: 'PUT', body: { autoreply: reply, forward: { to, keep_copy: rules.keepCopy } } })
    message.success(t('mailhost.rules.saved'))
    await load()
  } catch (err) {
    if (err instanceof ApiError && err.field) {
      ruleErrors.value = { [err.field]: err.message }
      tab.value = err.field === 'forward' ? 'forward' : 'reply'
    } else message.error(errText(err, t('mailhost.rules.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function saveQuota() {
  const e = editing.value
  if (!e) return
  const parsed = mailboxForm.shape.quota_mb.safeParse(e.quota ?? Number.NaN)
  if (!parsed.success) {
    e.error = resolveMessage(parsed.error.issues[0]?.message ?? '')
    return
  }
  saving.value = true
  try {
    await api(`/api/mail/mailboxes/${e.box.id}`, { method: 'PATCH', body: { quota_mb: parsed.data } })
    message.success(t('mailhost.quotaSaved'))
    e.error = ''
    await load()
  } catch (err) {
    e.error = errText(err, t('mailhost.mailboxToggleFailed'))
  } finally {
    saving.value = false
  }
}

async function savePassword(generate: boolean) {
  const e = editing.value
  if (!e) return
  saving.value = true
  try {
    const r = await api(`/api/mail/mailboxes/${e.box.id}/password`, { method: 'PUT', body: { password: generate ? '' : e.password }, schema: mailPasswordSchema })
    if (r.password) shownPassword.value = { address: e.address, password: r.password }
    message.success(t('mailhost.passwordSaved'))
    e.password = ''
    e.error = ''
    if (r.password) editing.value = null
  } catch (err) {
    e.error = errText(err, t('mailhost.passwordFailed'))
  } finally {
    saving.value = false
  }
}

// --- алиасы ---
const aliasForms = reactive<Record<number, { local: string; to: string }>>({})
const aliasErrors = reactive<Record<number, Record<string, string>>>({})
const savingAlias = reactive<Record<number, boolean>>({})

const aliasForm = (d: MailDomain) => (aliasForms[d.id] ??= { local: '', to: '' })

async function saveAlias(d: MailDomain) {
  const f = aliasForm(d)
  const parsed = mailAliasForm.safeParse(f)
  aliasErrors[d.id] = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  savingAlias[d.id] = true
  try {
    await api(`/api/mail/domains/${d.id}/aliases`, { method: 'PUT', body: { local: parsed.data.local, to: parsed.data.to.split(/[\s,;]+/).filter(Boolean) } })
    f.local = ''
    f.to = ''
    message.success(t('mailhost.aliasSaved'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) aliasErrors[d.id] = { [e.field]: e.message }
    else message.error(errText(e, t('mailhost.aliasFailed')))
  } finally {
    savingAlias[d.id] = false
  }
}

function editAlias(d: MailDomain, a: MailAlias) {
  const f = aliasForm(d)
  f.local = a.local
  f.to = a.to.join(', ')
}

async function removeAlias(a: MailAlias) {
  try {
    await api(`/api/mail/aliases/${a.id}`, { method: 'DELETE' })
    message.success(t('mailhost.aliasRemoved'))
    await load()
  } catch (e) {
    message.error(errText(e, t('mailhost.removeFailed')))
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

// --- журнал доставки: читается по кнопке (исполнитель разбирает журнал exim) ---
const journals = reactive<Record<number, MailJournal>>({})
const journalBusy = reactive<Record<number, boolean>>({})
const journalKind = reactive<Record<number, string>>({})

async function loadJournal(d: MailDomain) {
  journalBusy[d.id] = true
  try {
    journals[d.id] = await api(`/api/mail/domains/${d.id}/log`, { schema: mailJournalSchema })
  } catch (e) {
    message.error(errText(e, t('mailhost.journal.loadFailed')))
  } finally {
    journalBusy[d.id] = false
  }
}

const shownEvents = (d: MailDomain) => (journals[d.id]?.events ?? []).filter((e) => !journalKind[d.id] || e.kind === journalKind[d.id])
const journalKindOptions = computed(() => [
  { label: t('mailhost.journal.filterAll'), value: '' },
  { label: t('mailhost.journal.kind.received'), value: 'received' },
  { label: t('mailhost.journal.kind.delivered'), value: 'delivered' },
  { label: t('mailhost.journal.kind.deferred'), value: 'deferred' },
  { label: t('mailhost.journal.kind.failed'), value: 'failed' },
  { label: t('mailhost.journal.kind.rejected'), value: 'rejected' },
])
const eventKindLabel = (k: string) =>
  k === 'received'
    ? t('mailhost.journal.kind.received')
    : k === 'delivered'
      ? t('mailhost.journal.kind.delivered')
      : k === 'deferred'
        ? t('mailhost.journal.kind.deferred')
        : k === 'failed'
          ? t('mailhost.journal.kind.failed')
          : t('mailhost.journal.kind.rejected')
const viaLabel = (v: string) =>
  v === 'mailbox'
    ? t('mailhost.journal.via.mailbox')
    : v === 'remote'
      ? t('mailhost.journal.via.remote')
      : v === 'auth'
        ? t('mailhost.journal.via.auth')
        : v === 'local'
          ? t('mailhost.journal.via.local')
          : v === 'smtp'
            ? t('mailhost.journal.via.smtp')
            : ''

// Ключи перечислены явно: сторож i18n ищет их в коде как строки.
const kindLabel = (k: MailRecord['kind']) =>
  k === 'mx' ? t('mailhost.dnsKind.mx') : k === 'spf' ? t('mailhost.dnsKind.spf') : k === 'dkim' ? t('mailhost.dnsKind.dkim') : t('mailhost.dnsKind.dmarc')
const stateLabel = (s: MailRecord['state']) => (s === 'ok' ? t('mailhost.dnsStatus.ok') : s === 'missing' ? t('mailhost.dnsStatus.missing') : t('mailhost.dnsStatus.mismatch'))

const mb = (bytes: number) => Math.round(bytes / (1 << 20))
const dnsOk = (d: MailDomain) => (records[d.id]?.length ? records[d.id]!.every((r) => r.state === 'ok') : null)
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('mailhost.title') }}</h1>
      <p class="note">{{ t('mailhost.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <template v-if="info">
      <section class="glass card rise" style="--i: 1">
        <h2>{{ t('mailhost.domains') }}</h2>
        <p class="note">{{ t('mailhost.domainsLimit', { used: domains.length, max: info.max_domains }) }}</p>
        <n-form v-if="eligibleOptions.length" class="form inline" @submit.prevent="enableDomain">
          <n-form-item :label="t('mailhost.domain')" :validation-status="domainErrors.domain ? 'error' : undefined" :feedback="domainErrors.domain ? resolveMessage(domainErrors.domain) : undefined">
            <n-select
              v-model:value="domainForm.domain"
              :options="eligibleOptions"
              :placeholder="t('mailhost.domainPlaceholder')"
              :aria-label="t('mailhost.domain')"
              data-testid="mail-domain-select"
              @update:value="domainErrors.domain = ''"
            />
          </n-form-item>
          <n-button type="primary" attr-type="submit" :loading="enabling" data-testid="mail-domain-add">
            <template #icon><n-icon :component="AddOutline" /></template>
            {{ t('mailhost.enableDomain') }}
          </n-button>
        </n-form>
        <p v-else-if="!domains.length" class="note">{{ t('mailhost.noEligible') }}</p>
        <p v-if="!domains.length && eligibleOptions.length" class="note">{{ t('mailhost.noDomains') }}</p>
      </section>

      <section v-for="(d, i) in domains" :key="d.id" class="glass card rise" :style="`--i: ${i + 2}`" :data-testid="`mail-domain-${d.domain}`">
        <div class="row">
          <span class="ic"><n-icon :size="16" :component="MailOutline" /></span>
          <h2>{{ d.domain }}</h2>
          <span v-if="!d.enabled" class="badge off">{{ t('mailhost.disabledBadge') }}</span>
          <span v-else-if="dnsOk(d) === true" class="badge ok"><n-icon :component="CheckmarkCircleOutline" /> {{ t('mailhost.dnsStatus.ok') }}</span>
          <span v-else-if="dnsOk(d) === false" class="badge warn"><n-icon :component="WarningOutline" /> {{ t('mailhost.dnsTitle') }}</span>
          <span class="grow" />
          <n-button size="small" class="tint-amber" @click="toggleDomain(d)">{{ d.enabled ? t('mailhost.turnOff') : t('mailhost.turnOn') }}</n-button>
          <n-popconfirm @positive-click="removeDomain(d)">
            <template #trigger>
              <n-button size="small" class="tint-rose">{{ t('mailhost.removeDomain') }}</n-button>
            </template>
            {{ t('mailhost.removeDomainConfirm', { name: d.domain }) }}
          </n-popconfirm>
        </div>

        <div class="block">
          <div class="row">
            <h3>{{ t('mailhost.dnsTitle') }}</h3>
            <span class="grow" />
            <n-button size="small" class="tint-cyan" :loading="checking[d.id]" @click="checkDNS(d)">
              <template #icon><n-icon :component="RefreshOutline" /></template>
              {{ t('mailhost.dnsCheck') }}
            </n-button>
          </div>
          <p class="note">{{ t('mailhost.dnsHint') }}</p>
          <div v-if="autos[d.id]?.available" class="auto" :data-testid="`dns-auto-${d.domain}`">
            <strong>{{ t('mailhost.auto.title') }}</strong>
            <template v-if="autos[d.id]!.delegated">
              <p class="note">{{ t('mailhost.auto.hintDelegated') }}</p>
              <n-button type="primary" size="small" :loading="autoBusy[d.id]" :data-testid="`dns-auto-button-${d.domain}`" @click="autoConfigure(d)">{{ t('mailhost.auto.button') }}</n-button>
            </template>
            <p v-else-if="autos[d.id]!.zone" class="note">{{ t('mailhost.auto.hintZoneOnly', { ns: autos[d.id]!.expected.join(', ') }) }}</p>
            <template v-else>
              <p class="note">{{ t('mailhost.auto.hintNoZone', { ns: autos[d.id]!.expected.join(', ') }) }}</p>
              <router-link :to="{ name: 'dns' }" class="link">{{ t('mailhost.auto.openDns') }}</router-link>
            </template>
          </div>
          <ul v-if="records[d.id]?.length" class="dns">
            <li v-for="r in records[d.id]" :key="r.kind" :class="['rec', r.state]" :data-testid="`dns-${d.domain}-${r.kind}`">
              <div class="row">
                <strong>{{ kindLabel(r.kind) }}</strong>
                <span :class="['badge', r.state]">{{ stateLabel(r.state) }}</span>
              </div>
              <div class="kv"><span>{{ t('mailhost.dnsType') }}</span><code>{{ r.type }}</code></div>
              <div class="kv"><span>{{ t('mailhost.dnsName') }}</span><code>{{ r.name }}</code></div>
              <div class="kv">
                <span>{{ t('mailhost.dnsValue') }}</span>
                <code class="val">{{ r.value }}</code>
                <n-button size="tiny" quaternary :aria-label="t('common.copy')" @click="copyText(r.value)"><n-icon :component="CopyOutline" /></n-button>
              </div>
              <p v-if="r.detail" class="note">{{ t('mailhost.dnsFound', { value: r.detail }) }}</p>
            </li>
          </ul>
        </div>

        <div class="block">
          <h3>{{ t('mailhost.mailboxes') }}</h3>
          <p class="note">{{ t('mailhost.mailboxesLimit', { used: boxCount, max: info.max_mailboxes }) }}</p>
          <p v-if="!d.mailboxes.length" class="note">{{ t('mailhost.noMailboxes') }}</p>
          <ul v-if="d.mailboxes.length" class="items">
            <li v-for="b in d.mailboxes" :key="b.id" class="item" :data-testid="`mailbox-${b.local}@${d.domain}`">
              <div class="row">
                <strong>{{ b.local }}@{{ d.domain }}</strong>
                <span v-if="!b.enabled" class="badge off">{{ t('mailhost.mailboxOff') }}</span>
                <span v-if="b.autoreply.enabled" class="badge ok" data-testid="badge-autoreply">{{ t('mailhost.rules.badgeAutoReply') }}</span>
                <span v-if="b.forward.to.length" class="badge ok" data-testid="badge-forward">{{ t('mailhost.rules.badgeForward') }}</span>
                <span class="note">{{ t('mailhost.used', { used: mb(b.used_bytes), quota: b.quota_mb }) }}</span>
                <span class="grow" />
                <n-button size="small" class="tint-violet" @click="edit(d, b)">{{ t('mailhost.edit') }}</n-button>
                <n-button size="small" class="tint-amber" @click="toggleMailbox(b)">{{ b.enabled ? t('mailhost.disableMailbox') : t('mailhost.enableMailbox') }}</n-button>
                <n-popconfirm @positive-click="removeMailbox(b)">
                  <template #trigger>
                    <n-button size="small" class="tint-rose">{{ t('mailhost.removeMailbox') }}</n-button>
                  </template>
                  {{ t('mailhost.removeMailboxConfirm', { name: `${b.local}@${d.domain}` }) }}
                </n-popconfirm>
              </div>
            </li>
          </ul>
          <n-form class="form" @submit.prevent="createMailbox(d)">
            <n-form-item :label="t('mailhost.local')" :validation-status="boxErrors[d.id]?.local ? 'error' : undefined" :feedback="boxErrors[d.id]?.local ? resolveMessage(boxErrors[d.id]!.local!) : undefined">
              <n-input v-model:value="boxForm(d).local" :placeholder="t('mailhost.localPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('mailhost.local') }" />
              <span class="at">@{{ d.domain }}</span>
            </n-form-item>
            <n-form-item
              :label="t('mailhost.password')"
              :validation-status="boxErrors[d.id]?.password ? 'error' : undefined"
              :feedback="boxErrors[d.id]?.password ? resolveMessage(boxErrors[d.id]!.password!) : t('mailhost.passwordHint', { min: info.min_password })"
            >
              <n-input v-model:value="boxForm(d).password" type="password" show-password-on="click" :placeholder="t('mailhost.passwordPlaceholder')" autocomplete="new-password" :input-props="{ 'aria-label': t('mailhost.password') }" />
            </n-form-item>
            <n-form-item :label="t('mailhost.quota')" :validation-status="boxErrors[d.id]?.quota_mb ? 'error' : undefined" :feedback="boxErrors[d.id]?.quota_mb ? resolveMessage(boxErrors[d.id]!.quota_mb!) : undefined">
              <n-input-number v-model:value="boxForm(d).quota" :min="info.min_quota_mb" :max="info.max_quota_mb" :input-props="{ 'aria-label': t('mailhost.quota') }" />
            </n-form-item>
            <n-button type="primary" attr-type="submit" :loading="creating[d.id]" data-testid="mailbox-add">
              <template #icon><n-icon :component="AddOutline" /></template>
              {{ t('mailhost.addMailbox') }}
            </n-button>
          </n-form>
        </div>

        <div class="block">
          <h3>{{ t('mailhost.aliases') }}</h3>
          <p class="note">{{ t('mailhost.aliasesLimit', { used: aliasCount, max: info.max_aliases }) }} · {{ t('mailhost.aliasesHint') }}</p>
          <p v-if="!d.aliases.length" class="note">{{ t('mailhost.noAliases') }}</p>
          <ul v-if="d.aliases.length" class="items">
            <li v-for="a in d.aliases" :key="a.id" class="item" :data-testid="`alias-${a.local}@${d.domain}`">
              <div class="row">
                <strong>{{ a.local }}@{{ d.domain }}</strong>
                <span class="note">→ {{ a.to.join(', ') }}</span>
                <span class="grow" />
                <n-button size="small" class="tint-violet" @click="editAlias(d, a)">{{ t('mailhost.edit') }}</n-button>
                <n-popconfirm @positive-click="removeAlias(a)">
                  <template #trigger>
                    <n-button size="small" class="tint-rose">{{ t('mailhost.removeMailbox') }}</n-button>
                  </template>
                  {{ a.local }}@{{ d.domain }}?
                </n-popconfirm>
              </div>
            </li>
          </ul>
          <n-form class="form" @submit.prevent="saveAlias(d)">
            <n-form-item :label="t('mailhost.aliasLocal')" :validation-status="aliasErrors[d.id]?.local ? 'error' : undefined" :feedback="aliasErrors[d.id]?.local ? resolveMessage(aliasErrors[d.id]!.local!) : undefined">
              <n-input v-model:value="aliasForm(d).local" :placeholder="t('mailhost.aliasLocalPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('mailhost.aliasLocal') }" />
              <span class="at">@{{ d.domain }}</span>
            </n-form-item>
            <n-form-item
              :label="t('mailhost.aliasTo')"
              :validation-status="aliasErrors[d.id]?.to ? 'error' : undefined"
              :feedback="aliasErrors[d.id]?.to ? resolveMessage(aliasErrors[d.id]!.to!) : t('mailhost.aliasToHint')"
            >
              <n-input v-model:value="aliasForm(d).to" :placeholder="t('mailhost.aliasToPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('mailhost.aliasTo') }" />
            </n-form-item>
            <n-button type="primary" attr-type="submit" :loading="savingAlias[d.id]" data-testid="alias-save">{{ t('mailhost.saveAlias') }}</n-button>
          </n-form>
        </div>

        <div class="block">
          <div class="row">
            <h3>{{ t('mailhost.journal.title') }}</h3>
            <span class="grow" />
            <n-select v-if="journals[d.id]" v-model:value="journalKind[d.id]" :options="journalKindOptions" size="small" style="width: 190px" :aria-label="t('mailhost.journal.title')" />
            <n-button size="small" class="tint-cyan" :loading="journalBusy[d.id]" :data-testid="`journal-load-${d.domain}`" @click="loadJournal(d)">
              <template #icon><n-icon :component="RefreshOutline" /></template>
              {{ journals[d.id] ? t('mailhost.journal.refresh') : t('mailhost.journal.show') }}
            </n-button>
          </div>
          <p class="note">{{ t('mailhost.journal.hint') }}</p>
          <template v-if="journals[d.id]">
            <p v-if="!shownEvents(d).length" class="note">{{ t('mailhost.journal.empty') }}</p>
            <ul v-else class="items log" :data-testid="`journal-${d.domain}`">
              <li v-for="(ev, k) in shownEvents(d)" :key="k" class="ev">
                <span class="note ts">{{ ev.t }}</span>
                <span :class="['badge', ev.kind === 'delivered' ? 'ok' : ev.kind === 'received' ? 'off' : ev.kind === 'deferred' ? 'warn' : 'mismatch']">{{ eventKindLabel(ev.kind) }}</span>
                <span class="addr">{{ ev.from || '—' }} → {{ ev.to || '—' }}</span>
                <span v-if="viaLabel(ev.via)" class="note">{{ viaLabel(ev.via) }}</span>
                <span v-if="ev.detail" class="note detail">{{ ev.detail }}</span>
              </li>
            </ul>
            <h3>{{ t('mailhost.journal.queueTitle') }}</h3>
            <p v-if="!journals[d.id]!.queue.length" class="note">{{ t('mailhost.journal.queueEmpty') }}</p>
            <ul v-else class="items">
              <li v-for="q in journals[d.id]!.queue" :key="q.id" class="ev">
                <span class="addr">{{ q.from || '—' }} → {{ q.to.join(', ') }}</span>
                <span class="note">{{ t('mailhost.journal.queueItem', { age: q.age, size: q.size }) }}</span>
                <span v-if="q.frozen" class="badge mismatch">{{ t('mailhost.journal.frozen') }}</span>
              </li>
            </ul>
          </template>
        </div>
      </section>

      <section class="glass card rise" style="--i: 9">
        <h2>{{ t('mailhost.clientTitle') }}</h2>
        <p class="note">{{ t('mailhost.clientHint') }}</p>
        <p class="kv"><span>{{ t('mailhost.server') }}</span><code>{{ info.host }}</code></p>
        <p class="note">{{ t('mailhost.imap', { port: info.imap_port }) }}</p>
        <p class="note">{{ t('mailhost.pop3', { port: info.pop3_port }) }}</p>
        <p class="note">{{ t('mailhost.smtp', { ports: info.smtp_ports.join(' / ') }) }}</p>
        <p class="note">{{ t('mailhost.forwardNote') }}</p>
        <template v-if="info.webmail_url">
          <p class="note">{{ t('mailhost.webmailHint') }}</p>
          <n-button tag="a" :href="info.webmail_url" target="_blank" rel="noopener noreferrer" class="tint-violet" data-testid="webmail-link">{{ t('mailhost.openWebmail') }}</n-button>
        </template>
      </section>
    </template>

    <n-modal :show="editing !== null" preset="card" :title="editing?.address" style="max-width: 600px" @update:show="editing = null">
      <template v-if="editing">
        <n-tabs v-model:value="tab" type="line" animated>
          <n-tab-pane name="main" :tab="t('mailhost.rules.tabMain')">
            <n-form @submit.prevent="saveQuota">
              <n-form-item :label="t('mailhost.quota')">
                <n-space :size="10">
                  <n-input-number v-model:value="editing.quota" :min="info?.min_quota_mb" :max="info?.max_quota_mb" :input-props="{ 'aria-label': t('mailhost.quota') }" />
                  <n-button class="tint-cyan" :loading="saving" @click="saveQuota">{{ t('mailhost.setQuota') }}</n-button>
                </n-space>
              </n-form-item>
              <n-form-item :label="t('mailhost.newPassword')">
                <n-input v-model:value="editing.password" type="password" show-password-on="click" autocomplete="new-password" :input-props="{ 'aria-label': t('mailhost.newPassword') }" />
              </n-form-item>
              <n-space :size="10">
                <n-button type="primary" :loading="saving" :disabled="!editing.password" @click="savePassword(false)">{{ t('mailhost.save') }}</n-button>
                <n-button class="tint-violet" :loading="saving" data-testid="mailbox-generate" @click="savePassword(true)">{{ t('mailhost.generatePassword') }}</n-button>
              </n-space>
              <p v-if="editing.error" class="err">{{ editing.error }}</p>
            </n-form>
          </n-tab-pane>

          <n-tab-pane name="reply" :tab="t('mailhost.rules.tabAutoReply')">
            <n-form @submit.prevent="saveRules">
              <n-form-item :label="t('mailhost.rules.autoReply')" :feedback="t('mailhost.rules.autoReplyHint')">
                <n-switch v-model:value="rules.replyOn" :aria-label="t('mailhost.rules.autoReply')" data-testid="rules-reply-on" />
              </n-form-item>
              <template v-if="rules.replyOn">
                <n-form-item :label="t('mailhost.rules.subject')" :validation-status="ruleErrors.subject ? 'error' : undefined" :feedback="ruleErrors.subject ? resolveMessage(ruleErrors.subject) : undefined">
                  <n-input v-model:value="rules.subject" :placeholder="t('mailhost.rules.subjectPlaceholder')" :input-props="{ 'aria-label': t('mailhost.rules.subject') }" @update:value="ruleErrors.subject = ''" />
                </n-form-item>
                <n-form-item :label="t('mailhost.rules.body')" :validation-status="ruleErrors.body ? 'error' : undefined" :feedback="ruleErrors.body ? resolveMessage(ruleErrors.body) : undefined">
                  <n-input v-model:value="rules.body" type="textarea" :rows="4" :placeholder="t('mailhost.rules.bodyPlaceholder')" :input-props="{ 'aria-label': t('mailhost.rules.body') }" @update:value="ruleErrors.body = ''" />
                </n-form-item>
                <n-space :size="12">
                  <n-form-item :label="t('mailhost.rules.from')" :validation-status="ruleErrors.from ? 'error' : undefined" :feedback="ruleErrors.from ? resolveMessage(ruleErrors.from) : undefined">
                    <n-date-picker v-model:formatted-value="rules.from" type="date" value-format="yyyy-MM-dd" clearable :input-props="{ 'aria-label': t('mailhost.rules.from') }" />
                  </n-form-item>
                  <n-form-item :label="t('mailhost.rules.to')">
                    <n-date-picker v-model:formatted-value="rules.to" type="date" value-format="yyyy-MM-dd" clearable :input-props="{ 'aria-label': t('mailhost.rules.to') }" />
                  </n-form-item>
                </n-space>
                <p class="note">{{ t('mailhost.rules.datesHint') }}</p>
                <n-form-item :label="t('mailhost.rules.days')" :validation-status="ruleErrors.days ? 'error' : undefined" :feedback="ruleErrors.days ? resolveMessage(ruleErrors.days) : undefined">
                  <n-input-number v-model:value="rules.days" :min="1" :max="30" :input-props="{ 'aria-label': t('mailhost.rules.days') }" />
                </n-form-item>
              </template>
              <n-button type="primary" attr-type="submit" :loading="saving" data-testid="rules-save-reply">{{ t('mailhost.rules.save') }}</n-button>
            </n-form>
          </n-tab-pane>

          <n-tab-pane name="forward" :tab="t('mailhost.rules.tabForward')">
            <n-form @submit.prevent="saveRules">
              <n-form-item
                :label="t('mailhost.rules.forwardTo')"
                :validation-status="ruleErrors.forward ? 'error' : undefined"
                :feedback="ruleErrors.forward ? resolveMessage(ruleErrors.forward) : t('mailhost.rules.forwardHint')"
              >
                <n-input v-model:value="rules.forwardTo" :placeholder="t('mailhost.rules.forwardToPlaceholder')" :input-props="{ 'aria-label': t('mailhost.rules.forwardTo') }" @update:value="ruleErrors.forward = ''" />
              </n-form-item>
              <n-form-item :feedback="t('mailhost.rules.keepCopyHint')">
                <n-checkbox v-model:checked="rules.keepCopy">{{ t('mailhost.rules.keepCopy') }}</n-checkbox>
              </n-form-item>
              <n-button type="primary" attr-type="submit" :loading="saving" data-testid="rules-save-forward">{{ t('mailhost.rules.save') }}</n-button>
            </n-form>
          </n-tab-pane>
        </n-tabs>
      </template>
    </n-modal>

    <n-modal :show="shownPassword !== null" preset="card" :title="t('mailhost.passwordTitle')" style="max-width: 560px" :mask-closable="false" @update:show="shownPassword = null">
      <template v-if="shownPassword">
        <p class="note">{{ t('mailhost.passwordShownOnce') }}</p>
        <p class="kv"><span>{{ shownPassword.address }}</span></p>
        <pre class="pass" data-testid="mailbox-password">{{ shownPassword.password }}</pre>
        <n-space :size="10">
          <n-button class="tint-violet" @click="copyText(shownPassword.password)">
            <template #icon><n-icon :component="CopyOutline" /></template>
            {{ t('common.copy') }}
          </n-button>
          <n-button type="primary" data-testid="mailbox-password-close" @click="shownPassword = null">{{ t('mailhost.close') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 940px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

h2 {
  font-size: 19px;
  font-weight: 700;
  word-break: break-all;
}

h3 {
  font-size: 15px;
  font-weight: 700;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.block {
  display: grid;
  gap: 10px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.grow {
  flex: 1;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  color: #fff;
  background: var(--grad-primary);
}

.badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  background: rgba(148, 163, 184, 0.18);
}

.badge.ok {
  background: rgba(52, 211, 153, 0.18);
  color: #6ee7b7;
}

.badge.warn,
.badge.missing {
  background: rgba(251, 191, 36, 0.18);
  color: #fcd34d;
}

.badge.mismatch {
  background: rgba(251, 113, 133, 0.18);
  color: #fda4af;
}

.auto {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px dashed var(--border);
}

.link {
  font-size: 13.5px;
}

.dns,
.items {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.rec {
  display: grid;
  gap: 6px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid var(--border);
}

.kv {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}

.kv > span:first-child {
  min-width: 70px;
  color: var(--text-dim);
  font-size: 13px;
}

.val {
  word-break: break-all;
  font-size: 12px;
}

.ev {
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex-wrap: wrap;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}

.ts {
  font-variant-numeric: tabular-nums;
}

.addr {
  word-break: break-all;
}

.detail {
  flex-basis: 100%;
  word-break: break-word;
}

.item {
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.form {
  max-width: 620px;
}

.form.inline {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.form.inline :deep(.n-form-item) {
  min-width: 260px;
}

.at {
  margin-left: 8px;
  color: var(--text-dim);
  white-space: nowrap;
}

.pass {
  margin: 12px 0;
  padding: 12px;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--border);
  font-size: 15px;
  word-break: break-all;
  white-space: pre-wrap;
}

.err {
  margin-top: 10px;
  color: #fda4af;
  font-size: 13.5px;
}
</style>
