<script setup lang="ts">
import { AddOutline, CheckmarkCircleOutline, CopyOutline } from '@vicons/ionicons5'
import { NButton, NDataTable, NIcon, useMessage, type DataTableColumns } from 'naive-ui'
import { computed, h, onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { createdInviteSchema, invitesSchema, type Invite } from '@/api/schemas'
import EmptyState from '@/components/EmptyState.vue'
import FlagIcon from '@/components/FlagIcon.vue'
import StatusChip from '@/components/StatusChip.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { formatDateTime, LOCALES, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale, auto, setLocale } = useI18n()
const auth = useAuthStore()
const message = useMessage()
const invites = ref<Invite[]>([])
const creating = ref(false)

const registerLink = (code: string) => `${location.origin}/register?invite=${encodeURIComponent(code)}`
const isValid = (r: Invite) => r.used_by === null && new Date(r.expires_at) > new Date()

async function copy(code: string) {
  try {
    await navigator.clipboard.writeText(registerLink(code))
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}

const columns = computed<DataTableColumns<Invite>>(() => [
  { title: t('settings.invites.code'), key: 'code', render: (r) => h('code', r.code) },
  {
    title: t('settings.invites.status'),
    key: 'status',
    render: (r) => {
      if (r.used_by !== null) {
        const who = r.used_by_username ? t('settings.invites.usedBy', { name: r.used_by_username }) : t('settings.invites.usedByDeleted')
        return h(StatusChip, { tone: 'violet' }, () => who)
      }
      if (!isValid(r)) return h(StatusChip, { tone: 'slate' }, () => t('settings.invites.expired'))
      return h(StatusChip, { tone: 'emerald' }, () => t('settings.invites.validUntil', { date: formatDateTime(r.expires_at, locale.value) }))
    },
  },
  {
    title: '',
    key: 'actions',
    width: 190,
    render: (r) =>
      isValid(r)
        ? h(
            NButton,
            { size: 'small', class: 'tint-cyan', onClick: () => copy(r.code) },
            { default: () => t('settings.invites.copyLink'), icon: () => h(NIcon, null, { default: () => h(CopyOutline) }) },
          )
        : null,
  },
])

async function load() {
  try {
    invites.value = (await api('/api/invites', { schema: invitesSchema })).invites
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('settings.invites.loadFailed'))
  }
}

async function create() {
  creating.value = true
  try {
    await api('/api/invites', { method: 'POST', body: {}, schema: createdInviteSchema })
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('settings.invites.createFailed'))
  } finally {
    creating.value = false
  }
}

onMounted(() => {
  if (auth.isAdmin) void load()
})
</script>

<template>
  <div v-if="auth.user" class="page">
    <header class="head rise">
      <h1>{{ t('settings.title') }}</h1>
      <p>{{ t('settings.subtitle') }}</p>
    </header>

    <section class="profile glass rise" style="--i: 1">
      <user-avatar :name="auth.user.username" :size="76" />
      <div class="who">
        <h2>{{ auth.user.username }}</h2>
        <div class="mail">{{ auth.user.email }}</div>
        <status-chip :tone="auth.isAdmin ? 'violet' : 'cyan'">
          {{ auth.isAdmin ? t('settings.roleAdmin') : t('settings.roleUser') }}
        </status-chip>
      </div>
    </section>

    <section class="lang glass rise" style="--i: 2">
      <h3>{{ t('settings.language') }}</h3>
      <div class="tiles">
        <button
          v-for="l in LOCALES"
          :key="l"
          type="button"
          class="tile"
          :class="{ on: l === locale }"
          :aria-pressed="l === locale"
          :lang="l"
          @click="setLocale(l)"
        >
          <flag-icon :locale="l" :size="30" />
          <span class="name">{{ t(l === 'ru' ? 'lang.ru' : 'lang.it') }}</span>
          <n-icon v-if="l === locale" class="ok" :size="22" :component="CheckmarkCircleOutline" />
        </button>
      </div>
      <p v-if="auto" class="hint">{{ t('lang.autoHint') }}</p>
    </section>

    <section v-if="auth.isAdmin" class="invites glass rise" style="--i: 3">
      <div class="inv-head">
        <div>
          <h3>{{ t('settings.invites.title') }}</h3>
          <p class="hint">{{ t('settings.invites.hint') }}</p>
        </div>
        <n-button type="primary" :loading="creating" @click="create">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('settings.invites.create') }} · {{ t('settings.invites.createHint') }}
        </n-button>
      </div>
      <n-data-table v-if="invites.length" :columns="columns" :data="invites" :bordered="false" size="small" />
      <empty-state v-else :title="t('settings.invites.empty')" />
    </section>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 1080px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.head p {
  margin: 4px 0 0;
  color: var(--text-dim);
}

.profile {
  display: flex;
  align-items: center;
  gap: 22px;
  padding: 26px 28px;
}

.who {
  display: grid;
  gap: 6px;
  justify-items: start;
}

.who h2 {
  font-size: 24px;
  font-weight: 800;
}

.mail {
  color: var(--text-dim);
}

.lang,
.invites {
  padding: 22px 26px;
}

h3 {
  font-size: 17px;
  margin-bottom: 14px;
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 14px;
}

.tile {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  border: 1px solid var(--border);
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.04);
  color: var(--text);
  font: inherit;
  font-weight: 650;
  cursor: pointer;
  transition:
    transform 0.3s var(--ease),
    background 0.3s,
    border-color 0.3s,
    box-shadow 0.3s;
}

.tile:hover {
  transform: translateY(-3px);
  background: rgba(167, 139, 250, 0.12);
  border-color: rgba(167, 139, 250, 0.5);
}

.tile.on {
  background: linear-gradient(135deg, rgba(99, 102, 241, 0.28), rgba(236, 72, 153, 0.2));
  border-color: rgba(167, 139, 250, 0.8);
  box-shadow: 0 14px 34px -14px rgba(139, 92, 246, 0.8);
}

.name {
  flex: 1;
  text-align: left;
  font-size: 16px;
}

.ok {
  color: var(--emerald);
}

.hint {
  margin: 12px 0 0;
  font-size: 13.5px;
  color: var(--text-dim);
}

.inv-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.inv-head h3 {
  margin-bottom: 2px;
}

.inv-head .hint {
  margin: 0;
}
</style>
