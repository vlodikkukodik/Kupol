<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import type { DashboardItem } from '@/api/generated/documents'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
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

const CARDS = ['draft', 'review', 'published', 'archived'] as const
const count = (status: (typeof CARDS)[number]) => desk.value?.counts[status] ?? 0
const nothingMine = computed(() => CARDS.every((c) => count(c) === 0))

const label = (it: DashboardItem) => (it.code ? t('desk.withCode', { title: it.title, code: it.code }) : it.title)
const to = (it: DashboardItem, tab?: string) => ({ name: 'team-document', params: { id: it.id }, query: tab ? { tab } : {} })
</script>

<template>
  <ErrorView v-if="query.isError.value" :request-id="requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiSheet v-else-if="!desk"><UiSkeleton :lines="6" :label="$t('desk.loading')" /></UiSheet>

  <div v-else class="desk" data-testid="desk">
    <UiSheet as="section" aria-labelledby="mine-title">
      <div class="head">
        <h2 id="mine-title">{{ $t('desk.mineTitle') }}</h2>
        <UiButton v-if="desk.can_write" :to="{ name: 'team-document-new' }" variant="primary" icon="plus" data-testid="desk-new">{{ $t('desk.newDoc') }}</UiButton>
      </div>
      <UiEmpty v-if="nothingMine" icon="file" :title="$t('desk.noneTitle')">
        <template v-if="desk.can_write">{{ $t('desk.noneWrite') }}</template>
        <template v-else>{{ $t('desk.noneRead') }}</template>
      </UiEmpty>
      <ul v-else class="cards" :aria-label="$t('desk.cardsLabel')">
        <li v-for="c in CARDS" :key="c">
          <RouterLink :to="{ name: 'team-documents', query: { status: c, mine: '1' } }" class="card" :data-status="c" :data-testid="`desk-count-${c}`">
            <span class="card__n">{{ count(c) }}</span>
            <span class="card__label">{{ $t(`desk.card.${c}`) }}</span>
          </RouterLink>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.returned.length" as="section" aria-labelledby="returned-title" class="returned" data-testid="desk-returned">
      <h2 id="returned-title">{{ $t('desk.returnedTitle') }} <span class="n">{{ desk.returned.length }}</span></h2>
      <ul class="list">
        <li v-for="it in desk.returned" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="reason"><strong>{{ it.returned_by ?? $t('desk.reviewer') }}:</strong> {{ it.return_note }}</p>
          <p class="meta">
            <RouterLink v-if="it.open_comments" :to="to(it, 'review')" class="comments">{{ $t('desk.toFix', { n: it.open_comments }, it.open_comments) }}</RouterLink>
            <span v-else>{{ $t('desk.noComments') }}</span>
          </p>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.drafts.length" as="section" aria-labelledby="drafts-title" data-testid="desk-drafts">
      <h2 id="drafts-title">{{ $t('desk.draftsTitle') }}</h2>
      <ul class="list">
        <li v-for="it in desk.drafts" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="meta">{{ $t('desk.draftMeta', { type: it.type_name, when: formatDateTime(it.updated_at) }) }}</p>
        </li>
      </ul>
      <p v-if="desk.counts.draft > desk.drafts.length" class="more">
        <RouterLink :to="{ name: 'team-documents', query: { status: 'draft', mine: '1' } }">{{ $t('desk.allDrafts', { n: desk.counts.draft }) }}</RouterLink>
      </p>
    </UiSheet>

    <UiSheet v-if="desk.in_review.length" as="section" aria-labelledby="waiting-title" data-testid="desk-in-review">
      <h2 id="waiting-title">{{ $t('desk.waitingTitle') }}</h2>
      <ul class="list">
        <li v-for="it in desk.in_review" :key="it.id" :data-doc="it.id">
          <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
          <p class="meta">
            {{ $t('desk.sentAt', { when: it.submitted_at ? formatDateTime(it.submitted_at) : formatDateTime(it.updated_at) }) }}
            <template v-if="it.open_comments"> · <RouterLink :to="to(it, 'review')">{{ $t('desk.comments', { n: it.open_comments }, it.open_comments) }}</RouterLink></template>
          </p>
        </li>
      </ul>
    </UiSheet>

    <UiSheet v-if="desk.can_review" as="section" aria-labelledby="queue-title" data-testid="desk-queue">
      <h2 id="queue-title">{{ $t('desk.queueTitle') }} <span class="n" data-testid="desk-queue-total">{{ desk.queue_total }}</span></h2>
      <UiEmpty v-if="!desk.queue.length" icon="check" :title="$t('desk.queueEmpty')">{{ $t('desk.queueEmptyText') }}</UiEmpty>
      <template v-else>
        <p class="hint">{{ $t('desk.queueHint') }}</p>
        <ul class="list">
          <li v-for="it in desk.queue" :key="it.id" :data-doc="it.id">
            <RouterLink :to="to(it)" class="title">{{ label(it) }}</RouterLink>
            <p class="meta">
              <UiBadge tone="review">{{ $t('desk.inReview') }}</UiBadge>
              {{ $t('desk.queueMeta', { type: it.type_name, author: it.author ?? $t('desk.unknownAuthor'), when: it.submitted_at ? formatDateTime(it.submitted_at) : '—' }) }}
              <template v-if="it.open_comments">{{ $t('desk.openComments', { n: it.open_comments }) }}</template>
            </p>
          </li>
        </ul>
        <p v-if="desk.queue_total > desk.queue.length" class="more">
          <RouterLink :to="{ name: 'team-documents', query: { status: 'review' } }">{{ $t('desk.allQueue', { n: desk.queue_total }) }}</RouterLink>
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
