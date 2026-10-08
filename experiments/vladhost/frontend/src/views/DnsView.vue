<script setup lang="ts">
import { AddOutline, CheckmarkCircleOutline, CopyOutline, RefreshOutline, WarningOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NInputNumber, NPopconfirm, NSelect, NSpace, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import {
  dnsDelegationSchema,
  dnsDomainForm,
  dnsOverviewSchema,
  dnsRecordForm,
  dnsRecordResponseSchema,
  dnsZoneResponseSchema,
  fieldErrors,
  type DnsDelegation,
  type DnsRecord,
  type DnsZone,
} from '@/api/schemas'
import { resolveMessage, useI18n } from '@/i18n'

const { t } = useI18n()
const message = useMessage()

type Overview = ReturnType<typeof dnsOverviewSchema.parse>
const overview = ref<Overview | null>(null)
const loadError = ref('')
const info = computed(() => overview.value?.info)
const zones = computed<DnsZone[]>(() => overview.value?.zones ?? [])
const eligibleOptions = computed(() => (info.value?.eligible_domains ?? []).map((d) => ({ label: d, value: d })))
const typeOptions = computed(() => (info.value?.types ?? []).map((v) => ({ label: v, value: v })))
const ttlOptions = computed(() => (info.value?.ttls ?? []).map((n) => ({ label: t('dns.ttlSeconds', { n }), value: n })))

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    overview.value = await api('/api/dns', { schema: dnsOverviewSchema })
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('dns.loadFailed'))
  }
}
onMounted(load)

// --- зоны ---
const zoneForm = reactive({ domain: null as string | null })
const zoneErrors = ref<Record<string, string>>({})
const creating = ref(false)

async function addZone() {
  const parsed = dnsDomainForm.safeParse({ domain: zoneForm.domain ?? '' })
  zoneErrors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  creating.value = true
  try {
    await api('/api/dns/zones', { method: 'POST', body: parsed.data, schema: dnsZoneResponseSchema })
    zoneForm.domain = null
    message.success(t('dns.zoneCreated'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) zoneErrors.value = { [e.field]: e.message }
    else message.error(errText(e, t('dns.zoneFailed')))
  } finally {
    creating.value = false
  }
}

async function removeZone(z: DnsZone) {
  try {
    await api(`/api/dns/zones/${z.id}`, { method: 'DELETE' })
    message.success(t('dns.zoneRemoved'))
    await load()
  } catch (e) {
    message.error(errText(e, t('dns.removeFailed')))
  }
}

// --- делегирование ---
const delegations = reactive<Record<number, DnsDelegation>>({})
const checking = reactive<Record<number, boolean>>({})

async function checkDelegation(z: DnsZone) {
  checking[z.id] = true
  try {
    delegations[z.id] = (await api(`/api/dns/zones/${z.id}/delegation`, { schema: dnsDelegationSchema })).delegation
  } catch (e) {
    message.error(errText(e, t('dns.delegationFailed')))
  } finally {
    checking[z.id] = false
  }
}

// Ключи перечислены явно: сторож i18n ищет их в коде как строки.
const stateLabel = (s: DnsDelegation['state']) =>
  s === 'ok' ? t('dns.state.ok') : s === 'partial' ? t('dns.state.partial') : s === 'mixed' ? t('dns.state.mixed') : s === 'none' ? t('dns.state.none') : t('dns.state.unknown')
const hintFor = (type: string) =>
  type === 'A'
    ? t('dns.hint_a')
    : type === 'AAAA'
      ? t('dns.hint_aaaa')
      : type === 'CNAME'
        ? t('dns.hint_cname')
        : type === 'MX'
          ? t('dns.hint_mx')
          : type === 'SRV'
            ? t('dns.hint_srv')
            : type === 'CAA'
              ? t('dns.hint_caa')
              : t('dns.hint_txt')

// --- записи ---
interface Form {
  id: number
  name: string
  type: string
  value: string
  priority: number | null
  ttl: number | null
}
const forms = reactive<Record<number, Form>>({})
const recordErrors = reactive<Record<number, Record<string, string>>>({})
const saving = reactive<Record<number, boolean>>({})

const formOf = (z: DnsZone) => (forms[z.id] ??= { id: 0, name: '', type: 'A', value: '', priority: 10, ttl: 300 })
const usesPriority = (type: string) => type === 'MX' || type === 'SRV'

async function saveRecord(z: DnsZone) {
  const f = formOf(z)
  const parsed = dnsRecordForm.safeParse({ name: f.name, type: f.type, value: f.value, priority: usesPriority(f.type) ? (f.priority ?? Number.NaN) : 0, ttl: f.ttl ?? Number.NaN })
  recordErrors[z.id] = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  saving[z.id] = true
  try {
    const url = f.id ? `/api/dns/records/${f.id}` : `/api/dns/zones/${z.id}/records`
    await api(url, { method: f.id ? 'PUT' : 'POST', body: parsed.data, schema: dnsRecordResponseSchema })
    message.success(t('dns.recordSaved'))
    Object.assign(f, { id: 0, name: '', value: '', priority: 10 })
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) recordErrors[z.id] = { [e.field]: e.message }
    else message.error(errText(e, t('dns.recordFailed')))
  } finally {
    saving[z.id] = false
  }
}

function editRecord(z: DnsZone, r: DnsRecord) {
  Object.assign(formOf(z), { id: r.id, name: r.name === '@' ? '' : r.name, type: r.type, value: r.value, priority: r.priority, ttl: r.ttl })
  recordErrors[z.id] = {}
}

function cancelEdit(z: DnsZone) {
  Object.assign(formOf(z), { id: 0, name: '', value: '', priority: 10 })
  recordErrors[z.id] = {}
}

async function removeRecord(r: DnsRecord) {
  try {
    await api(`/api/dns/records/${r.id}`, { method: 'DELETE' })
    message.success(t('dns.recordRemoved'))
    await load()
  } catch (e) {
    message.error(errText(e, t('dns.removeFailed')))
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
      <h1>{{ t('dns.title') }}</h1>
      <p class="note">{{ t('dns.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <template v-if="info">
      <section class="glass card rise" style="--i: 1">
        <h2>{{ t('dns.nsTitle') }}</h2>
        <ul class="ns" data-testid="dns-ns">
          <li v-for="n in info.ns" :key="n">
            <code>{{ n }}</code>
            <n-button size="tiny" quaternary :aria-label="t('common.copy')" @click="copyText(n)"><n-icon :component="CopyOutline" /></n-button>
          </li>
        </ul>
        <p class="note">{{ t('dns.nsHint') }}</p>
      </section>

      <section class="glass card rise" style="--i: 2">
        <h2>{{ t('dns.zones') }}</h2>
        <p class="note">{{ t('dns.zonesLimit', { used: zones.length, max: info.max_zones }) }}</p>
        <n-form v-if="eligibleOptions.length" class="form inline" @submit.prevent="addZone">
          <n-form-item :label="t('dns.domain')" :validation-status="zoneErrors.domain ? 'error' : undefined" :feedback="zoneErrors.domain ? resolveMessage(zoneErrors.domain) : undefined">
            <n-select v-model:value="zoneForm.domain" :options="eligibleOptions" :placeholder="t('dns.domainPlaceholder')" :aria-label="t('dns.domain')" data-testid="dns-domain-select" @update:value="zoneErrors.domain = ''" />
          </n-form-item>
          <n-button type="primary" attr-type="submit" :loading="creating" data-testid="dns-zone-add">
            <template #icon><n-icon :component="AddOutline" /></template>
            {{ t('dns.addZone') }}
          </n-button>
        </n-form>
        <p v-else-if="!zones.length" class="note">{{ t('dns.noEligible') }}</p>
        <p v-if="!zones.length && eligibleOptions.length" class="note">{{ t('dns.noZones') }}</p>
      </section>

      <section v-for="(z, i) in zones" :key="z.id" class="glass card rise" :style="`--i: ${i + 3}`" :data-testid="`dns-zone-${z.domain}`">
        <div class="row">
          <h2>{{ z.domain }}</h2>
          <span v-if="delegations[z.id]" class="note">{{ t('dns.delegation') }}:</span>
          <span v-if="delegations[z.id]" :class="['badge', delegations[z.id]!.state === 'ok' ? 'ok' : delegations[z.id]!.state === 'partial' ? 'warn' : 'mismatch']" :data-testid="`dns-state-${z.domain}`">
            <n-icon :component="delegations[z.id]!.state === 'ok' ? CheckmarkCircleOutline : WarningOutline" />
            {{ stateLabel(delegations[z.id]!.state) }}
          </span>
          <span class="grow" />
          <n-button size="small" class="tint-cyan" :loading="checking[z.id]" :data-testid="`dns-check-${z.domain}`" @click="checkDelegation(z)">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            {{ t('dns.checkDelegation') }}
          </n-button>
          <n-popconfirm @positive-click="removeZone(z)">
            <template #trigger>
              <n-button size="small" class="tint-rose">{{ t('dns.removeZone') }}</n-button>
            </template>
            {{ t('dns.removeZoneConfirm', { name: z.domain }) }}
          </n-popconfirm>
        </div>
        <template v-if="delegations[z.id]">
          <p class="note">{{ delegations[z.id]!.found.length ? t('dns.found', { value: delegations[z.id]!.found.join(', ') }) : t('dns.foundNone') }}</p>
          <p v-if="delegations[z.id]!.state !== 'ok'" class="note">{{ t('dns.expected', { value: delegations[z.id]!.expected.join(', ') }) }}</p>
        </template>

        <div class="block">
          <h3>{{ t('dns.records') }}</h3>
          <p class="note">{{ t('dns.recordsLimit', { used: z.records.length, max: info.max_records }) }}</p>
          <p v-if="!z.records.length" class="note">{{ t('dns.noRecords') }}</p>
          <ul v-else class="recs">
            <li v-for="r in z.records" :key="r.id" class="rec" :data-testid="`dns-record-${r.name}-${r.type}`">
              <span class="cell name">{{ r.name === '@' ? t('dns.apex') : r.name }}</span>
              <code class="cell type">{{ r.type }}</code>
              <code class="cell val">{{ usesPriority(r.type) ? `${r.priority} ` : '' }}{{ r.value }}</code>
              <span class="cell note">{{ t('dns.ttlSeconds', { n: r.ttl }) }}</span>
              <span v-if="r.managed === 'mail'" class="badge ok">{{ t('dns.managedMail') }}</span>
              <span class="grow" />
              <n-button size="tiny" class="tint-violet" @click="editRecord(z, r)">{{ t('dns.edit') }}</n-button>
              <n-popconfirm @positive-click="removeRecord(r)">
                <template #trigger>
                  <n-button size="tiny" class="tint-rose">{{ t('dns.remove') }}</n-button>
                </template>
                {{ t('dns.removeRecordConfirm', { type: r.type, name: r.name === '@' ? t('dns.apex') : r.name }) }}
              </n-popconfirm>
            </li>
          </ul>

          <n-form class="form" @submit.prevent="saveRecord(z)">
            <n-space :size="12" align="start">
              <n-form-item :label="t('dns.name')" :validation-status="recordErrors[z.id]?.name ? 'error' : undefined" :feedback="recordErrors[z.id]?.name ? resolveMessage(recordErrors[z.id]!.name!) : undefined">
                <n-input v-model:value="formOf(z).name" :placeholder="t('dns.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('dns.name') }" />
              </n-form-item>
              <n-form-item :label="t('dns.type')" :validation-status="recordErrors[z.id]?.type ? 'error' : undefined" :feedback="recordErrors[z.id]?.type ? resolveMessage(recordErrors[z.id]!.type!) : undefined">
                <n-select v-model:value="formOf(z).type" :options="typeOptions" style="width: 110px" :aria-label="t('dns.type')" :data-testid="`dns-type-${z.domain}`" />
              </n-form-item>
              <n-form-item v-if="usesPriority(formOf(z).type)" :label="t('dns.priority')" :validation-status="recordErrors[z.id]?.priority ? 'error' : undefined" :feedback="recordErrors[z.id]?.priority ? resolveMessage(recordErrors[z.id]!.priority!) : undefined">
                <n-input-number v-model:value="formOf(z).priority" :min="0" :max="65535" style="width: 110px" :input-props="{ 'aria-label': t('dns.priority') }" />
              </n-form-item>
              <n-form-item :label="t('dns.ttl')" :validation-status="recordErrors[z.id]?.ttl ? 'error' : undefined" :feedback="recordErrors[z.id]?.ttl ? resolveMessage(recordErrors[z.id]!.ttl!) : undefined">
                <n-select v-model:value="formOf(z).ttl" :options="ttlOptions" style="width: 110px" :aria-label="t('dns.ttl')" />
              </n-form-item>
            </n-space>
            <n-form-item :label="t('dns.value')" :validation-status="recordErrors[z.id]?.value ? 'error' : undefined" :feedback="recordErrors[z.id]?.value ? resolveMessage(recordErrors[z.id]!.value!) : hintFor(formOf(z).type)">
              <n-input v-model:value="formOf(z).value" autocomplete="off" :input-props="{ 'aria-label': t('dns.value'), spellcheck: false }" @update:value="recordErrors[z.id] = {}" />
            </n-form-item>
            <n-space :size="10">
              <n-button type="primary" attr-type="submit" :loading="saving[z.id]" :data-testid="`dns-save-${z.domain}`">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ formOf(z).id ? t('dns.saveRecord') : t('dns.addRecord') }}
              </n-button>
              <n-button v-if="formOf(z).id" @click="cancelEdit(z)">{{ t('dns.cancelEdit') }}</n-button>
            </n-space>
          </n-form>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 980px;
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

.ns {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.ns li {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 10px;
  border: 1px solid var(--border);
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

.badge.warn {
  background: rgba(251, 191, 36, 0.18);
  color: #fcd34d;
}

.badge.mismatch {
  background: rgba(251, 113, 133, 0.18);
  color: #fda4af;
}

.recs {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 6px;
}

.rec {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
  padding: 6px 0;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}

.cell.name {
  min-width: 90px;
  font-weight: 600;
  word-break: break-all;
}

.cell.type {
  min-width: 48px;
}

.cell.val {
  word-break: break-all;
  font-size: 12px;
  flex: 1 1 260px;
}

.form {
  max-width: 760px;
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
</style>
