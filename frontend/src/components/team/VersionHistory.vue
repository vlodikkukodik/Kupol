<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Diff, Problem, SaveResult, VersionItem } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import ErrorView from '@/views/ErrorView.vue'
import VersionDiff from './VersionDiff.vue'

const props = defineProps<{
  docId: number
  /** текущая редакция: при её смене история перечитывается */
  revision: number
  canRestore?: boolean
  statusName: (id: string) => string
  blockKindName: (id: string) => string
}>()
const emit = defineEmits<{ restored: [result: SaveResult]; problems: [problems: Problem[]] }>()

const PER_PAGE = 20
const page = ref(1)
const list = useQuery({
  queryKey: computed(() => [...keys.teamVersions(props.docId, page.value), props.revision] as const),
  queryFn: ({ signal }) => teamApi.versions(props.docId, { page: page.value, per_page: PER_PAGE }, { signal }),
  placeholderData: keepPreviousData,
  staleTime: 0,
})

// Открытое сравнение: с текущим содержимым или с предыдущей по списку версией.
const open = ref<{ id: number; against: 'live' | 'prev' } | null>(null)
const diff = ref<Diff | null>(null)
const diffError = ref('')
const loadingDiff = ref(false)
const confirming = ref<number | null>(null) // id версии, откат к которой подтверждается
const restoring = ref(false)
const restoreError = ref('')
watch([page, () => props.revision], () => {
  open.value = null
  diff.value = null
  confirming.value = null
})

function previousOf(id: number): VersionItem | null {
  const items = list.data.value?.items ?? []
  const i = items.findIndex((v) => v.id === id)
  return (i >= 0 ? items[i + 1] : undefined) ?? null
}

async function showDiff(v: VersionItem, against: 'live' | 'prev') {
  if (open.value?.id === v.id && open.value.against === against) {
    open.value = null
    return
  }
  open.value = { id: v.id, against }
  diff.value = null
  diffError.value = ''
  loadingDiff.value = true
  try {
    // «что изменилось в этой версии» = разница от предыдущей к этой; «с текущим» = от этой к документу
    const prev = against === 'prev' ? previousOf(v.id) : null
    const res = against === 'live' || !prev ? await teamApi.diff(props.docId, v.id, 'live') : await teamApi.diff(props.docId, prev.id, v.id)
    diff.value = res.diff
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    diffError.value = describeApiError(err)
  } finally {
    loadingDiff.value = false
  }
}

async function restore(v: VersionItem) {
  restoring.value = true
  restoreError.value = ''
  try {
    const res = await teamApi.restore(props.docId, v.id)
    confirming.value = null
    emit('restored', res)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.problems.length) {
      restoreError.value = 'Эту версию нельзя вернуть как есть: она не проходит проверку.'
      emit('problems', err.problems)
    } else {
      restoreError.value = describeApiError(err)
    }
  } finally {
    restoring.value = false
  }
}

const pages = computed(() => list.data.value?.pages ?? 1)
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
</script>

<template>
  <section class="history" aria-labelledby="history-title">
    <h3 id="history-title">История версий</h3>
    <p class="note">
      Снимки при создании, смене статуса, правке опубликованного и откате хранятся всегда; обычные сохранения и
      автосохранения — последние 30.
    </p>
    <UiAlert v-if="restoreError" tone="danger">{{ restoreError }}</UiAlert>

    <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
    <UiSkeleton v-else-if="list.isPending.value" :lines="5" label="Загрузка истории…" />

    <template v-else-if="list.data.value">
      <ol class="versions" data-testid="versions">
        <li v-for="v in list.data.value.items" :key="v.id" :data-version="v.id" :data-kind="v.kind">
          <div class="line">
            <span class="kind">{{ v.kind_name }}</span>
            <span class="meta">
              {{ formatDateTime(v.created_at) }} · редакция {{ v.revision }} · {{ statusName(v.status) }}<template v-if="v.author"> · {{ v.author }}</template>
            </span>
            <span v-if="v.note" class="note-line">{{ v.note }}</span>
          </div>
          <div class="actions">
            <UiButton variant="link" size="sm" :aria-expanded="open?.id === v.id && open.against === 'live' ? 'true' : 'false'" @click="showDiff(v, 'live')">
              Сравнить с текущей
            </UiButton>
            <UiButton v-if="previousOf(v.id)" variant="link" size="sm" :aria-expanded="open?.id === v.id && open.against === 'prev' ? 'true' : 'false'" @click="showDiff(v, 'prev')">
              Что изменилось
            </UiButton>
            <UiButton v-if="canRestore && confirming !== v.id" variant="link" size="sm" @click="confirming = v.id">Вернуть эту версию</UiButton>
          </div>
          <div v-if="confirming === v.id" class="confirm" role="group" :aria-label="`Подтверждение отката к версии ${v.id}`">
            <p>Документ станет таким, как в этой версии. Это будет новая редакция; нынешняя останется в истории.</p>
            <UiButton variant="primary" :loading="restoring" @click="restore(v)">{{ restoring ? 'Возвращаем…' : 'Да, вернуть' }}</UiButton>
            <UiButton variant="link" @click="confirming = null">Отмена</UiButton>
          </div>
          <div v-if="open?.id === v.id" class="diff-box">
            <UiSkeleton v-if="loadingDiff" :lines="3" label="Сравниваем…" />
            <UiAlert v-else-if="diffError" tone="danger">{{ diffError }}</UiAlert>
            <VersionDiff v-else-if="diff" :diff="diff" :block-kind-name="blockKindName" />
          </div>
        </li>
      </ol>
      <nav v-if="pages > 1" class="pager" aria-label="Страницы истории">
        <UiButton :disabled="page <= 1" @click="page--">← Новее</UiButton>
        <span>Страница {{ page }} из {{ pages }}</span>
        <UiButton :disabled="page >= pages" @click="page++">Старше →</UiButton>
      </nav>
    </template>
  </section>
</template>

<style scoped>
.note {
  color: var(--text-muted);
}
.versions {
  margin: 0;
  padding: 0;
  list-style: none;
}
.versions > li {
  padding: var(--space-3) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.line {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
}
.kind {
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.meta,
.note-line {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.note-line {
  flex-basis: 100%;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0 var(--space-4);
  margin-top: var(--space-1);
}
.confirm {
  margin-top: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--amber-700);
  border-radius: var(--radius-2);
  background: #f6ecd0;
}
.confirm p {
  margin-bottom: var(--space-2);
}
.confirm > :where(button, a) {
  margin-right: var(--space-3);
}
.diff-box {
  margin-top: var(--space-2);
  padding: 0 var(--space-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
}
.pager {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-4);
  margin-top: var(--space-4);
}
</style>
