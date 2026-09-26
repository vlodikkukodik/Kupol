<script setup lang="ts">
// Очередь предложений (шаг 5.4): право review — Редактор и Директорат. Статусы только вперёд
// (получено → рассмотрено → принято/отклонено); переход в «принято» начисляет автору XP, а пояснение
// в диалоге становится «запиской» автору. Своё предложение не разбираешь сам (как свой документ).
import { computed, ref, watch } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { suggestionsApi } from '@/api/endpoints'
import type { Out, Status } from '@/api/generated/suggestions'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UiAlert from '@/ui/UiAlert.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiModal from '@/ui/UiModal.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTabs from '@/ui/UiTabs.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from '../ErrorView.vue'

const PER_PAGE = 50
const MAX_COMMENT = 2000
const FILTERS = ['', 'received', 'reviewed', 'accepted', 'rejected']

/** Куда ведёт перевод: только вперёд, «принято» и «отклонено» окончательны. */
const NEXT: Record<Status, Status[]> = {
  received: ['reviewed', 'accepted', 'rejected'],
  reviewed: ['accepted', 'rejected'],
  accepted: [],
  rejected: [],
}
/** Подписи кнопок-решений (тексты — каталог сообщений). */
function actionLabel(st: Status): string {
  if (st === 'reviewed') return t('suggest.team.actionReviewed')
  if (st === 'accepted') return t('suggest.team.actionAccepted')
  return t('suggest.team.actionRejected')
}
const TONES: Record<Status, 'draft' | 'review' | 'published' | 'danger'> = {
  received: 'draft',
  reviewed: 'review',
  accepted: 'published',
  rejected: 'danger',
}

const auth = useAuthStore()
const client = useQueryClient()

const filter = ref('')
const page = ref(1)
watch(filter, () => {
  page.value = 1
})
const tabs = computed(() => FILTERS.map((id) => ({ id, label: id ? t(`suggest.status.${id}`) : t('suggest.team.filterAll') })))

const query = useQuery({
  queryKey: computed(() => keys.teamSuggestions(filter.value, page.value)),
  queryFn: ({ signal }) =>
    suggestionsApi.queue({ status: filter.value || undefined, page: page.value, per_page: PER_PAGE }, { signal }),
  staleTime: 0,
})
const data = computed(() => query.data.value ?? null)
const items = computed(() => data.value?.items ?? [])
const requestId = computed(() => (isApiError(query.error.value) ? query.error.value.requestId : ''))

const nextOf = (status: Status): Status[] => NEXT[status] ?? []
const isOwn = (item: Out): boolean => Boolean(item.author && item.author === auth.user?.login)

// Решение: диалог с необязательным пояснением (оно и есть «записка» автору)
const deciding = ref<{ item: Out; status: Status } | null>(null)
const comment = ref('')
const busy = ref(false)
const failure = ref('')
const notice = ref('')
const dialogOpen = computed({
  get: () => deciding.value !== null,
  set: (v: boolean) => {
    if (!v) deciding.value = null
  },
})

function start(item: Out, status: Status) {
  deciding.value = { item, status }
  comment.value = ''
  failure.value = ''
}

async function confirm() {
  const d = deciding.value
  if (!d || busy.value) return
  busy.value = true
  failure.value = ''
  try {
    await suggestionsApi.setStatus(d.item.id, { status: d.status, comment: comment.value })
    deciding.value = null
    notice.value = t('suggest.team.done', { status: t(`suggest.status.${d.status}`) })
    await client.invalidateQueries({ queryKey: keys.teamSuggestionsRoot })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    // сообщение сервера уже на языке читателя (invalid_state, self_review)
    failure.value = err.fields.status || err.message || describeApiError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UiPageHeader :title="$t('suggest.team.title')" :kicker="$t('suggest.team.kicker')" />

  <UiTabs v-model="filter" :tabs="tabs" :label="$t('suggest.team.filterLabel')" />

  <UiAlert v-if="notice" tone="success" data-testid="suggestions-notice">{{ notice }}</UiAlert>

  <UiSkeleton v-if="query.isPending.value" :lines="4" :label="$t('suggest.team.loading')" />
  <ErrorView v-else-if="query.isError.value" :request-id="requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiEmpty v-else-if="!items.length" :title="$t('suggest.team.empty')" />
  <template v-else>
    <p class="total">{{ $t('suggest.team.total', { n: data?.total ?? 0 }) }}</p>
    <ul class="list" data-testid="suggestions-list">
      <li v-for="item in items" :key="item.id">
        <UiSheet as="article" class="item">
          <p class="item__meta">
            <UiBadge :tone="TONES[item.status] ?? 'neutral'">{{ $t(`suggest.status.${item.status}`) }}</UiBadge>
            <span> · {{ $t('suggest.team.by', { author: item.author ?? '' }) }}</span>
            <span> · <time :datetime="item.created_at">{{ formatDateTime(item.created_at) }}</time></span>
          </p>
          <p class="item__text">{{ item.text }}</p>
          <p v-if="item.comment" class="item__note">{{ $t('suggest.mine.note') }}: {{ item.comment }}</p>
          <UiAlert v-if="isOwn(item)" tone="info" data-testid="suggestion-own">{{ $t('suggest.team.own') }}</UiAlert>
          <div v-else-if="nextOf(item.status).length" class="item__actions">
            <UiButton
              v-for="st in nextOf(item.status)"
              :key="st"
              size="sm"
              :variant="st === 'accepted' ? 'primary' : st === 'rejected' ? 'danger' : 'secondary'"
              :data-testid="`suggestion-${st}`"
              @click="start(item, st)"
            >
              {{ actionLabel(st) }}
            </UiButton>
          </div>
        </UiSheet>
      </li>
    </ul>

    <nav v-if="(data?.pages ?? 0) > 1" class="pager" :aria-label="$t('suggest.team.pager')">
      <UiButton :disabled="page <= 1" data-testid="suggestions-prev" @click="page--">{{ $t('suggest.team.prev') }}</UiButton>
      <span class="pager__page">{{ $t('suggest.team.pageInfo', { page: data?.page ?? 1, pages: data?.pages ?? 1 }) }}</span>
      <UiButton :disabled="page >= (data?.pages ?? 1)" data-testid="suggestions-next" @click="page++">{{ $t('suggest.team.next') }}</UiButton>
    </nav>
    <p class="note">{{ $t('suggest.team.note') }}</p>
  </template>

  <UiModal
    v-model:open="dialogOpen"
    :title="deciding ? $t('suggest.team.dialogTitle', { status: t(`suggest.status.${deciding.status}`) }) : ''"
    testid="suggestion-dialog"
  >
    <p v-if="deciding" class="dialog__text">{{ deciding.item.text }}</p>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
    <UiField :label="$t('suggest.team.comment')" :hint="$t('suggest.team.commentHint')">
      <UiTextarea v-model="comment" name="comment" :rows="3" :maxlength="MAX_COMMENT" data-testid="suggestion-comment" />
    </UiField>
    <div class="item__actions">
      <UiButton variant="primary" :loading="busy" data-testid="suggestion-confirm" @click="confirm">
        {{ $t('suggest.team.confirm') }}
      </UiButton>
      <UiButton variant="link" @click="dialogOpen = false">{{ $t('suggest.team.cancel') }}</UiButton>
    </div>
  </UiModal>
</template>

<style scoped>
.total {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-3);
}
.item {
  margin: 0;
  max-width: none;
}
.item__meta {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.item__text {
  margin: 0 0 var(--space-2);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.item__note {
  margin: 0 0 var(--space-2);
  padding-left: var(--space-3);
  border-left: 3px solid var(--border-strong);
  color: var(--text-muted);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.item__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.pager {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-4);
}
.pager__page {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.note {
  margin-top: var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.dialog__text {
  margin-top: 0;
  padding-left: var(--space-3);
  border-left: 3px solid var(--border-strong);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
