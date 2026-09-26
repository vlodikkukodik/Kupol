<script setup lang="ts">
// Активные сессии аккаунта: где выполнен вход, и кнопка «Завершить» для каждой, кроме текущей.
import { DesktopOutline, PhonePortraitOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NPopconfirm, useMessage } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { api, apiVoid, ApiError } from '@/api/client'
import { accountSessionsSchema, revokedSessionsSchema, type AccountSession } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, useI18n } from '@/i18n'
import { parseUserAgent } from '@/lib/useragent'

const { t, locale } = useI18n()
const message = useMessage()
const sessions = ref<AccountSession[]>([])
const loaded = ref(false)
const busy = ref<number | 'all' | null>(null)

const others = computed(() => sessions.value.filter((s) => !s.current).length)

function device(s: AccountSession) {
  const d = parseUserAgent(s.user_agent)
  const name = [d.browser, d.os].filter(Boolean).join(' · ')
  return { name: name || t('settings.sessions.unknownDevice'), mobile: d.mobile }
}

async function load() {
  try {
    sessions.value = (await api('/api/me/sessions', { schema: accountSessionsSchema })).sessions
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('settings.sessions.loadFailed'))
  } finally {
    loaded.value = true
  }
}
onMounted(load)

async function revoke(s: AccountSession) {
  busy.value = s.id
  try {
    await apiVoid(`/api/me/sessions/${s.id}`, { method: 'DELETE' })
    message.success(t('settings.sessions.revoked'))
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('settings.sessions.revokeFailed'))
  } finally {
    busy.value = null
  }
}

async function revokeOthers() {
  busy.value = 'all'
  try {
    const r = await api('/api/me/sessions/revoke-others', { method: 'POST', schema: revokedSessionsSchema })
    message.success(t('settings.sessions.revokedAll', { n: r.revoked }))
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('settings.sessions.revokeFailed'))
  } finally {
    busy.value = null
  }
}
</script>

<template>
  <section class="sessions glass rise" style="--i: 6">
    <div class="head">
      <div>
        <h3>{{ t('settings.sessions.title') }}</h3>
        <p class="hint">{{ t('settings.sessions.hint') }}</p>
      </div>
      <n-popconfirm v-if="others > 0" @positive-click="revokeOthers">
        <template #trigger>
          <n-button class="tint-rose" :loading="busy === 'all'">{{ t('settings.sessions.revokeOthers') }}</n-button>
        </template>
        {{ t('settings.sessions.revokeOthersConfirm', { n: others }) }}
      </n-popconfirm>
    </div>

    <ul v-if="loaded" class="list">
      <li v-for="s in sessions" :key="s.id" class="row">
        <n-icon class="icon" :size="26" :component="device(s).mobile ? PhonePortraitOutline : DesktopOutline" />
        <div class="info">
          <div class="name">
            {{ device(s).name }}
            <status-chip v-if="s.current" tone="emerald">{{ t('settings.sessions.current') }}</status-chip>
          </div>
          <div class="meta">
            <span v-if="s.ip">{{ s.ip }} · </span>
            {{ t('settings.sessions.lastSeen', { date: formatDateTime(s.last_seen_at, locale) }) }} ·
            {{ t('settings.sessions.signedIn', { date: formatDateTime(s.created_at, locale) }) }}
          </div>
        </div>
        <n-popconfirm v-if="!s.current" @positive-click="revoke(s)">
          <template #trigger>
            <n-button size="small" :loading="busy === s.id">{{ t('settings.sessions.revoke') }}</n-button>
          </template>
          {{ t('settings.sessions.revokeConfirm') }}
        </n-popconfirm>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.sessions {
  padding: 22px 26px;
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

h3 {
  font-size: 17px;
  margin-bottom: 2px;
}

.hint {
  margin: 0;
  font-size: 13.5px;
  color: var(--text-dim);
}

.list {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  display: grid;
}

.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 0;
  border-top: 1px solid var(--border);
}

.row:first-child {
  border-top: 0;
}

.icon {
  color: var(--text-dim);
  flex: none;
}

.info {
  flex: 1;
  min-width: 0;
}

.name {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-weight: 650;
}

.meta {
  margin-top: 3px;
  font-size: 13px;
  color: var(--text-dim);
  overflow-wrap: anywhere;
}
</style>
