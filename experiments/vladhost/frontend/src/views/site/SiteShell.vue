<script setup lang="ts">
import { NAlert, NButton, NSwitch, useMessage } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { shellResponseSchema, terminalTicketSchema, type ShellStatus, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import WebTerminal from '@/components/WebTerminal.vue'
import { useI18n } from '@/i18n'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const message = useMessage()

const url = computed(() => `/api/sites/${props.site.id}/shell`)
const status = ref<ShellStatus | null>(null)
const loadError = ref('')
const busy = ref(false)
const ticket = ref('')
const opening = ref(false)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    status.value = (await api(url.value, { schema: shellResponseSchema })).shell
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('terminal.loadFailed'))
  }
}
onMounted(load)

const sshCommand = computed(() => (status.value ? `ssh ${status.value.login}@${status.value.host} -p ${status.value.port}` : ''))

async function toggle(enabled: boolean) {
  busy.value = true
  try {
    status.value = (await api(url.value, { method: 'PUT', body: { enabled }, schema: shellResponseSchema })).shell
    if (!enabled) ticket.value = ''
    message.success(t(enabled ? 'terminal.turnedOn' : 'terminal.turnedOff'))
  } catch (e) {
    message.error(errText(e, t('terminal.enableFailed')))
  } finally {
    busy.value = false
  }
}

async function openTerminal() {
  opening.value = true
  try {
    ticket.value = (await api(`/api/sites/${props.site.id}/terminal`, { method: 'POST', schema: terminalTicketSchema })).ticket
  } catch (e) {
    message.error(errText(e, t('terminal.failed')))
  } finally {
    opening.value = false
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
      <h1>{{ t('terminal.title') }}</h1>
      <p class="note">{{ t('terminal.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <template v-if="status">
      <section class="glass card rise" style="--i: 1">
        <div class="row">
          <div class="grow">
            <h3>{{ t('terminal.enable') }}</h3>
            <p class="note">{{ t('terminal.enableHint') }}</p>
          </div>
          <status-chip :tone="status.enabled ? 'emerald' : 'slate'">{{ status.enabled ? t('terminal.enabled') : t('terminal.disabled') }}</status-chip>
          <n-switch :value="status.enabled" :loading="busy" :aria-label="t('terminal.enable')" @update:value="toggle" />
        </div>
      </section>

      <template v-if="status.enabled">
        <section class="glass card rise" style="--i: 2">
          <h3>{{ t('terminal.connect') }}</h3>
          <div class="cmdrow">
            <code class="cmd" data-testid="ssh-command">{{ sshCommand }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(sshCommand)">{{ t('common.copy') }}</n-button>
          </div>
          <p v-if="status.host_fingerprint" class="note">{{ t('terminal.fingerprint') }}: <code>{{ status.host_fingerprint }}</code></p>
          <n-alert v-if="status.keys === 0" type="warning" :show-icon="false">
            {{ t('terminal.noKeys') }}
            <router-link :to="{ name: 'ssh' }" class="link">{{ t('terminal.keysLink') }}</router-link>
          </n-alert>
          <p class="note">{{ t('terminal.sftp', { port: status.port }) }}</p>
        </section>

        <section class="glass card rise" style="--i: 3">
          <div class="row">
            <h3 class="grow">{{ t('terminal.title') }}</h3>
            <n-button v-if="!ticket" type="primary" :loading="opening" @click="openTerminal">{{ t('terminal.open') }}</n-button>
            <n-button v-else class="tint-rose" @click="ticket = ''">{{ t('terminal.close') }}</n-button>
          </div>
          <web-terminal v-if="ticket" :key="ticket" :ticket="ticket" />
          <p class="note">{{ t('terminal.limits') }}</p>
          <p class="note">{{ t('terminal.files') }}</p>
        </section>
      </template>
    </template>
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

.card h3 {
  font-size: 16px;
}

.row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.grow {
  flex: 1;
  min-width: 200px;
}

.cmdrow {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.cmd {
  padding: 8px 12px;
  border-radius: 10px;
  word-break: break-all;
}

.link {
  color: #a5f3fc;
  margin-left: 8px;
}
</style>
