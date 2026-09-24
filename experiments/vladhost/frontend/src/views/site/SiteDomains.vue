<script setup lang="ts">
import { AddOutline, CopyOutline, RefreshOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NPopconfirm, useMessage } from 'naive-ui'
import { computed, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { domainForm, domainResponseSchema, fieldErrors, type Domain, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { resolveMessage, useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const store = useSitesStore()
const message = useMessage()

const newDomain = ref('')
const error = ref('')
const adding = ref(false)
const domainBusy = ref<number | null>(null)
const serverIp = computed(() => store.domainConfig.server_ips.join(', '))

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)
const reload = () => store.load(t('sites.loadFailed'))

async function add() {
  const parsed = domainForm.safeParse({ host: newDomain.value })
  if (!parsed.success) {
    error.value = fieldErrors(parsed.error).host ?? ''
    return
  }
  error.value = ''
  adding.value = true
  try {
    await api(`/api/sites/${props.site.id}/domains`, { method: 'POST', body: { host: parsed.data.host }, schema: domainResponseSchema })
    newDomain.value = ''
    message.success(t('sites.domains.added'))
    await reload()
  } catch (e) {
    if (e instanceof ApiError && e.field === 'host') error.value = e.message
    else message.error(errText(e, t('sites.domains.addFailed')))
  } finally {
    adding.value = false
  }
}

async function check(d: Domain) {
  domainBusy.value = d.id
  try {
    await api(`/api/sites/${props.site.id}/domains/${d.id}/check`, { method: 'POST', schema: domainResponseSchema })
    await reload()
  } catch (e) {
    message.error(errText(e, t('sites.domains.checkFailed')))
  } finally {
    domainBusy.value = null
  }
}

async function remove(d: Domain) {
  domainBusy.value = d.id
  try {
    await api(`/api/sites/${props.site.id}/domains/${d.id}`, { method: 'DELETE' })
    await reload()
  } catch (e) {
    message.error(errText(e, t('sites.domains.removeFailed')))
  } finally {
    domainBusy.value = null
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

const tone = { pending_dns: 'amber', pending_cert: 'cyan', active: 'emerald', failed: 'rose' } as const
const statusKey = {
  pending_dns: 'sites.domains.status.pendingDns',
  pending_cert: 'sites.domains.status.pendingCert',
  active: 'sites.domains.status.active',
  failed: 'sites.domains.status.failed',
} as const
const problemKey = {
  no_a: 'sites.domains.problem.noA',
  wrong_ip: 'sites.domains.problem.wrongIp',
  has_aaaa: 'sites.domains.problem.hasAaaa',
  lookup: 'sites.domains.problem.lookup',
} as const

function problem(d: Domain): string {
  const key = problemKey[d.problem as keyof typeof problemKey]
  return key ? t(key, { found: d.found.join(', '), ip: serverIp.value }) : ''
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('sites.domains.title') }}</h1>
    </header>

    <n-alert v-if="!store.domainConfig.available" type="info" :show-icon="false">{{ t('siteArea.domainsUnavailable') }}</n-alert>

    <section v-else class="glass card rise" style="--i: 1">
      <p class="note">{{ t('sites.domains.hint', { ip: serverIp }) }}</p>

      <ul v-if="site.domains.length" class="dom-list">
        <li v-for="d in site.domains" :key="d.id" class="dom-row">
          <div class="dom-main">
            <a v-if="d.status === 'active'" :href="`https://${d.host}`" target="_blank" rel="noopener" class="dom-host">{{ d.host }}</a>
            <span v-else class="dom-host">{{ d.host }}</span>
            <status-chip :tone="tone[d.status]" :pulse="d.status === 'pending_dns' || d.status === 'pending_cert'">
              {{ t(statusKey[d.status]) }}
            </status-chip>
            <span class="grow" />
            <n-button v-if="d.status === 'pending_dns' || d.status === 'failed'" size="small" class="tint-cyan" :loading="domainBusy === d.id" @click="check(d)">
              <template #icon><n-icon :component="RefreshOutline" /></template>
              {{ t('sites.domains.check') }}
            </n-button>
            <n-popconfirm @positive-click="remove(d)">
              <template #trigger>
                <n-button size="small" class="tint-rose" :disabled="domainBusy === d.id">{{ t('sites.domains.remove') }}</n-button>
              </template>
              {{ t('sites.domains.removeConfirm', { host: d.host }) }}
            </n-popconfirm>
          </div>
          <template v-if="d.status === 'pending_dns'">
            <div class="dom-todo">
              <code>{{ t('sites.domains.instruction', { host: d.host, ip: serverIp }) }}</code>
              <n-button size="tiny" class="tint-violet" @click="copyText(store.domainConfig.server_ips[0] ?? '')">
                <template #icon><n-icon :component="CopyOutline" /></template>
                {{ t('sites.domains.copyIp') }}
              </n-button>
            </div>
            <p v-if="problem(d)" class="dom-problem">{{ problem(d) }}</p>
            <p class="note">{{ t('sites.domains.ttlNote') }}</p>
          </template>
          <p v-else-if="d.status === 'failed'" class="dom-problem">{{ t('sites.domains.problem.failedBody', { reason: d.error || t('sites.cert.unknownReason') }) }}</p>
        </li>
      </ul>
      <p v-else class="note">{{ t('sites.domains.empty') }}</p>

      <n-form class="dom-form" @submit.prevent="add">
        <n-form-item
          :label="t('sites.domains.label')"
          :validation-status="error ? 'error' : undefined"
          :feedback="error ? resolveMessage(error) : t('sites.domains.wwwHint')"
        >
          <n-input v-model:value="newDomain" :placeholder="t('sites.domains.placeholder')" autocomplete="off" :input-props="{ 'aria-label': t('sites.domains.label') }" />
        </n-form-item>
        <n-button type="primary" attr-type="submit" :loading="adding">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('sites.domains.add') }}
        </n-button>
      </n-form>
    </section>
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

.card {
  padding: 20px 24px;
}

.note {
  font-size: 13.5px;
  color: var(--text-dim);
}

.dom-list {
  list-style: none;
  margin: 10px 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.dom-row {
  padding: 10px 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.dom-main {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.dom-host {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.grow {
  flex: 1;
}

.dom-todo {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.dom-problem {
  margin: 8px 0 0;
  font-size: 13.5px;
  color: #fcd34d;
}

.dom-form {
  margin-top: 8px;
  max-width: 420px;
}
</style>
