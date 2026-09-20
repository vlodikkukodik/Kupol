<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import type { DashboardItem } from '@/api/generated/documents'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { formatDateTime } from '@/lib/format'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from '../ErrorView.vue'

// Рабочий стол: что ждёт человека сейчас. Автору — его документы по состояниям и то, что вернули на доработку (с причиной
// и числом открытых замечаний); рецензенту — ещё и очередь на проверку, давно ждущие сверху.
const query = useQuery({ queryKey: keys.teamDashboard, queryFn: ({ signal }) => teamApi.dashboard({ signal }), staleTime: 0 })
const desk = computed(() => query.data.value)
const requestId = computed(() => (isApiError(query.error.value) ? query.error.value.requestId : ''))

const CARDS = [
  { status: 'draft', label: 'Черновики' },
  { status: 'review', label: 'На проверке' },
  { status: 'published', label: 'Опубликовано' },
  { status: 'archived', label: 'В архиве' },
] as const
const count = (status: (typeof CARDS)[number]['status']) => desk.value?.counts[status] ?? 0
const nothingMine = computed(() => CARDS.every((c) => count(c.status) === 0))

const label = (it: DashboardItem) => `${it.title}${it.code ? ` (${it.code})` : ''}`
const to = (it: DashboardItem, tab?: string) => ({ name: 'team-document', params: { id: it.id }, query: tab ? { tab } : {} })
const commentsWord = (n: number) => (n % 10 === 1 && n % 100 !== 11 ? 'замечание' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 12 || n % 100 > 14) ? 'замечания' : 'замечаний')
</script>

<template>
  <ErrorView v-if="query.isError.value" :request-id="requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiSheet v-else-if="!desk"><UiSkeleton :lines="6" label="Собираем рабочий стол…" /></UiSheet>

  <div v-else class="desk" data-testid="desk">
    <UiSheet as="section" aria-labelledby="mine-title">
      <div class="head">
        <h2 id="mine-title">Мои документы</h2>
        <UiButton v-if="desk.can_write" :to="{ name: 'team-document-new' }" variant="primary" icon="plus" data-testid="desk-new">Новый документ</UiButton>
      </div>
      <UiEmpty v-if="nothingMine" icon="file" title="У вас пока нет документов">
        <template v-if="desk.can_write">Создайте первый черновик — он появится здесь.</template>
        <template v-else>Документы вам может передать только автор: писать их вправе Автор, Редактор и Директорат.</template>
      </UiEmpty>
      <ul v-else class="cards" aria-label="Мои документы по состояниям">
        <li v-for="c in CARDS" :key="c.status">
          <RouterLink :to="{ name: 'team-documents', query: { status: c.status, mine: '1' } }" class="card" :data-status="c.status" :data-testid="`desk-count-${c.status}`">
            <span class="card__n">{{ count(c.status) }}</span>
            <span class="card__label">{{ c.label }}</span>
          </RouterLink>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.returned.length" as="section" aria-labelledby="returned-title" class="returned" data-testid="desk-returned">
      <h2 id="returned-title">Вернули на доработку <span class="n">{{ desk.returned.length }}</span></h2>
      <ul class="list">
        <li v-for="it in desk.returned" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="reason"><strong>{{ it.returned_by ?? 'Рецензент' }}:</strong> {{ it.return_note }}</p>
          <p class="meta">
            <RouterLink v-if="it.open_comments" :to="to(it, 'review')" class="comments">{{ it.open_comments }} {{ commentsWord(it.open_comments) }} к исправлению</RouterLink>
            <span v-else>Замечаний в тексте нет — только причина возврата.</span>
          </p>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.drafts.length" as="section" aria-labelledby="drafts-title" data-testid="desk-drafts">
      <h2 id="drafts-title">Черновики</h2>
      <ul class="list">
        <li v-for="it in desk.drafts" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="meta">{{ it.type_name }} · изменён {{ formatDateTime(it.updated_at) }}</p>
        </li>
      </ul>
      <p v-if="desk.counts.draft > desk.drafts.length" class="more">
        <RouterLink :to="{ name: 'team-documents', query: { status: 'draft', mine: '1' } }">Все черновики ({{ desk.counts.draft }})</RouterLink>
      </p>
    </UiSheet>

    <UiSheet v-if="desk.in_review.length" as="section" aria-labelledby="waiting-title" data-testid="desk-in-review">
      <h2 id="waiting-title">Ждут проверки</h2>
      <ul class="list">
        <li v-for="it in desk.in_review" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="meta">
            Отправлен {{ it.submitted_at ? formatDateTime(it.submitted_at) : formatDateTime(it.updated_at) }}
            <template v-if="it.open_comments"> · <RouterLink :to="to(it, 'review')">{{ it.open_comments }} {{ commentsWord(it.open_comments) }}</RouterLink></template>
          </p>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.can_review" as="section" aria-labelledby="queue-title" data-testid="desk-queue">
      <h2 id="queue-title">Очередь на проверку <span class="n" data-testid="desk-queue-total">{{ desk.queue_total }}</span></h2>
      <UiEmpty v-if="!desk.queue.length" icon="check" title="Очередь пуста">Всё, что отправили на проверку, уже разобрано. Свои документы вы проверять не можете — их проверяет другой Редактор.</UiEmpty>
      <template v-else>
        <p class="hint">Давно ждущие — сверху. Свои документы в очередь не попадают.</p>
        <ul class="list">
          <li v-for="it in desk.queue" :key="it.id" :data-doc="it.id">
            <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
            <p class="meta">
              <UiBadge tone="review">На проверке</UiBadge>
              {{ it.type_name }} · автор {{ it.author ?? 'неизвестен' }} · отправлен {{ it.submitted_at ? formatDateTime(it.submitted_at) : '—' }}
              <template v-if="it.open_comments"> · открытых замечаний: {{ it.open_comments }}</template>
            </p>
          </li>
        </ul>
        <p v-if="desk.queue_total > desk.queue.length" class="more">
          <RouterLink :to="{ name: 'team-documents', query: { status: 'review' } }">Вся очередь ({{ desk.queue_total }})</RouterLink>
        </p>
      </template>
    </UiSheet>
  </div>
</template>

<style scoped>
.desk {
  display: grid;
  gap: var(--space-5);
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.head h2,
.desk h2 {
  margin: 0;
}
.n {
  color: var(--text-muted);
  font-weight: 400;
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
  gap: var(--space-3);
  margin: 0;
  padding: 0;
  list-style: none;
}
.card {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-height: 5rem;
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
  color: var(--text);
  text-decoration: none;
}
.card:hover {
  border-color: var(--ink-900);
}
.card__n {
  font-family: var(--font-head);
  font-size: var(--text-3xl);
  font-weight: 700;
  line-height: 1;
}
.card__label {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.returned {
  border-left: 6px solid var(--red-700);
}
.list {
  margin: var(--space-3) 0 0;
  padding: 0;
  list-style: none;
}
.list li {
  padding: var(--space-3) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.list li:last-child {
  border-bottom: 0;
}
.title {
  font-weight: 700;
  overflow-wrap: anywhere;
}
.reason {
  margin: var(--space-1) 0 0;
  padding-left: var(--space-3);
  border-left: 3px solid var(--red-700);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.meta {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.comments {
  font-weight: 700;
}
.hint {
  margin: var(--space-2) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.more {
  margin: var(--space-2) 0 0;
}
</style>
