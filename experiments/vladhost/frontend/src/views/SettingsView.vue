<script setup lang="ts">
import { NButton, NCard, NDataTable, NDescriptions, NDescriptionsItem, NEmpty, useMessage, type DataTableColumns } from 'naive-ui'
import { h, onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { createdInviteSchema, invitesSchema, type Invite } from '@/api/schemas'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const message = useMessage()
const invites = ref<Invite[]>([])
const creating = ref(false)

const fmt = (iso: string) => new Date(iso).toLocaleString('ru-RU')
const registerLink = (code: string) => `${location.origin}/register?invite=${encodeURIComponent(code)}`

async function copy(code: string) {
  try {
    await navigator.clipboard.writeText(registerLink(code))
    message.success('Ссылка скопирована')
  } catch {
    message.error('Не удалось скопировать — выделите ссылку вручную')
  }
}

const columns: DataTableColumns<Invite> = [
  { title: 'Код', key: 'code', render: (r) => h('code', r.code) },
  {
    title: 'Статус',
    key: 'status',
    render: (r) =>
      r.used_by !== null
        ? `использован: ${r.used_by_username ?? 'удалённый пользователь'}`
        : new Date(r.expires_at) < new Date()
          ? 'истёк'
          : `действует до ${fmt(r.expires_at)}`,
  },
  {
    title: '',
    key: 'actions',
    render: (r) =>
      r.used_by === null && new Date(r.expires_at) > new Date()
        ? h(NButton, { size: 'tiny', onClick: () => copy(r.code) }, () => 'Копировать ссылку')
        : null,
  },
]

async function load() {
  try {
    invites.value = (await api('/api/invites', { schema: invitesSchema })).invites
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'Не удалось загрузить инвайты')
  }
}

async function create() {
  creating.value = true
  try {
    await api('/api/invites', { method: 'POST', body: {}, schema: createdInviteSchema })
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : 'Не удалось создать инвайт')
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
    <h2>Настройки</h2>
    <n-card title="Аккаунт" size="small">
      <n-descriptions :column="1" label-placement="left">
        <n-descriptions-item label="Имя пользователя">{{ auth.user.username }}</n-descriptions-item>
        <n-descriptions-item label="Email">{{ auth.user.email }}</n-descriptions-item>
        <n-descriptions-item label="Роль">{{ auth.isAdmin ? 'администратор' : 'пользователь' }}</n-descriptions-item>
      </n-descriptions>
    </n-card>

    <n-card v-if="auth.isAdmin" title="Приглашения" size="small" class="block">
      <template #header-extra>
        <n-button type="primary" size="small" :loading="creating" @click="create">Создать инвайт (7 дней)</n-button>
      </template>
      <n-data-table v-if="invites.length" :columns="columns" :data="invites" :bordered="false" size="small" />
      <n-empty v-else description="Инвайтов пока нет" />
    </n-card>
  </div>
</template>

<style scoped>
.page { max-width: 720px; }
h2 { margin: 0 0 16px; }
.block { margin-top: 16px; }
</style>
