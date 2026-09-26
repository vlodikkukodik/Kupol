<script setup lang="ts">
// Внутренняя почта (шаг 5.7): записки Директората и автоматические уведомления (повышение допуска, грамоты,
// решения по предложениям, ответы на пометки). Тексты автоматических записок сервер собирает на языке читателя.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { RouterLink } from 'vue-router'
import { isApiError } from '@/api/client'
import { inboxApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { formatDateTime } from '@/lib/format'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from './ErrorView.vue'

const client = useQueryClient()
const page = ref(1)
const list = useQuery({ queryKey: computed(() => keys.inbox(page.value)), queryFn: ({ signal }) => inboxApi.list(page.value, { signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

async function refresh() {
  await client.invalidateQueries({ queryKey: ['me', 'inbox'] })
}
async function read(id: number) {
  await inboxApi.markRead(id)
  await refresh()
}
async function readAll() {
  await inboxApi.markAllRead()
  await refresh()
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <div v-else>
    <UiPageHeader :title="$t('inbox.title')" :kicker="$t('inbox.kicker')" />
    <UiSheet as="section" aria-labelledby="inbox-title" data-testid="inbox">
      <h2 id="inbox-title" class="visually-hidden">{{ $t('inbox.title') }}</h2>
      <UiSkeleton v-if="list.isPending.value" :lines="4" :label="$t('inbox.loading')" />
      <UiEmpty v-else-if="!list.data.value?.items.length" icon="mail" :title="$t('inbox.empty')" />
      <template v-else>
        <div class="bar">
          <span data-testid="inbox-unread">{{ $t('inbox.unread', { n: list.data.value.unread }) }}</span>
          <UiButton v-if="list.data.value.unread" variant="link" data-testid="inbox-read-all" @click="readAll">{{ $t('inbox.readAll') }}</UiButton>
        </div>
        <ul class="messages">
          <li v-for="m in list.data.value.items" :key="m.id" :class="{ 'is-unread': !m.read }" :data-message="m.id">
            <div class="messages__head">
              <strong>{{ m.title }}</strong>
              <time :datetime="m.created_at">{{ formatDateTime(m.created_at) }}</time>
            </div>
            <p class="messages__body">{{ m.body }}</p>
            <div class="messages__actions">
              <RouterLink v-if="m.link" :to="m.link" @click="!m.read && read(m.id)">{{ $t('inbox.open') }}</RouterLink>
              <UiButton v-if="!m.read" variant="link" size="sm" @click="read(m.id)">{{ $t('inbox.markRead') }}</UiButton>
            </div>
          </li>
        </ul>
        <nav v-if="list.data.value.pages > 1" class="pager" :aria-label="$t('inbox.pages')">
          <UiButton :disabled="page <= 1" @click="page--">{{ $t('inbox.prev') }}</UiButton>
          <span>{{ page }} / {{ list.data.value.pages }}</span>
          <UiButton :disabled="page >= list.data.value.pages" @click="page++">{{ $t('inbox.next') }}</UiButton>
        </nav>
      </template>
    </UiSheet>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  color: var(--text-muted);
}
.messages {
  margin: 0;
  padding: 0;
  list-style: none;
}
.messages li {
  padding: var(--space-3) var(--space-3);
  border-top: 1px dashed var(--border-strong);
}
.messages li:first-child {
  border-top: 0;
}
.messages li.is-unread {
  border-left: 4px solid var(--red-700);
  background: var(--surface-sunken);
}
.messages__head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-2);
}
.messages__head time {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.messages__body {
  margin: var(--space-1) 0;
  white-space: pre-line;
  overflow-wrap: anywhere;
}
.messages__actions,
.pager {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.pager {
  justify-content: center;
  margin-top: var(--space-4);
}
</style>
