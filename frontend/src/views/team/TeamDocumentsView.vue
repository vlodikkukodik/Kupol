<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import PaginationNav from '@/components/PaginationNav.vue'
import ImportDocumentDialog from '@/components/team/ImportDocumentDialog.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTable from '@/ui/UiTable.vue'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { useDocumentMeta } from '@/composables/useDocumentMeta'
import { useQueryFilters } from '@/composables/useQueryFilters'
import { formatDateTime, formatTime } from '@/lib/format'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import ErrorView from '../ErrorView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { statuses, types, statusName } = useDocumentMeta()
const importOpen = ref(false)

const one = (v: unknown): string => (Array.isArray(v) ? String(v[0] ?? '') : typeof v === 'string' ? v : '')
const status = computed(() => one(route.query.status))
const type = computed(() => one(route.query.type))
const q = computed(() => one(route.query.q))
const mine = computed(() => one(route.query.mine) === '1')
const page = computed(() => Math.max(1, Number.parseInt(one(route.query.page), 10) || 1))

// Состояние списка — в адресе: /team/documents?status=review&mine=1&page=2
const list = useQuery({
  queryKey: computed(() => keys.teamDocuments(`${status.value}|${type.value}|${q.value}|${mine.value}|${page.value}`)),
  queryFn: ({ signal }) =>
    teamApi.documents({ status: status.value, type: type.value, q: q.value, mine: mine.value, page: page.value, per_page: 25 }, { signal }),
  placeholderData: keepPreviousData,
})

const filters = useQueryFilters()
const search = ref(q.value)
watch(q, (v) => (search.value = v))
const active = computed(() => Boolean(status.value || type.value || q.value || mine.value))
const reset = () => router.push({ query: {} })
const statusOptions = computed(() => statuses.value.map((s) => ({ value: s.id, label: s.name })))
const typeOptions = computed(() => types.value.map((it) => ({ value: it.id, label: it.name })))
const note = computed(() => t('docs.noteMine') + (auth.can('review') || auth.can('edit_published') ? t('docs.noteAll') : '') + t('docs.noteEnd'))
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const tone = (s: string) => (s === 'published' ? 'published' : s === 'review' ? 'review' : s === 'archived' ? 'archived' : 'draft')
</script>

<template>
  <UiSheet as="section" wide aria-labelledby="docs-title" class="wide">
    <div class="head">
      <h2 id="docs-title">{{ $t('docs.title') }}</h2>
      <div v-if="auth.can('write_drafts')" class="head__actions">
        <UiButton icon="upload" data-testid="import-open" @click="importOpen = true">{{ $t('docs.import') }}</UiButton>
        <UiButton variant="primary" icon="plus" :to="{ name: 'team-document-new' }">{{ $t('docs.newDoc') }}</UiButton>
      </div>
    </div>
    <p class="note">{{ note }}</p>

    <form class="filters" :aria-label="$t('docs.filters')" @submit.prevent="filters.change({ q: search.trim() })">
      <UiField id="d-q" :label="$t('docs.nameOrCode')">
        <UiInput v-model="search" type="search" :maxlength="100" @change="filters.change({ q: search.trim() })" />
      </UiField>
      <UiField id="d-status" :label="$t('docs.status')">
        <UiSelect :model-value="status" :options="statusOptions" :placeholder="$t('docs.anyOne')" @update:model-value="filters.change({ status: $event })" />
      </UiField>
      <UiField id="d-type" :label="$t('docs.type')">
        <UiSelect :model-value="type" :options="typeOptions" :placeholder="$t('docs.anyOne')" @update:model-value="filters.change({ type: $event })" />
      </UiField>
      <div class="check">
        <UiCheckbox :model-value="mine" :label="$t('docs.onlyMine')" @update:model-value="filters.change({ mine: $event ? '1' : '' })" />
      </div>
      <div class="actions">
        <UiButton type="submit" variant="primary">{{ $t('docs.find') }}</UiButton>
        <UiButton v-if="active" variant="link" @click="reset">{{ $t('docs.reset') }}</UiButton>
      </div>
    </form>

    <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
    <UiSkeleton v-else-if="list.isPending.value" :lines="6" />

    <template v-else-if="list.data.value">
      <p class="total" role="status" data-testid="team-docs-total">{{ $t('docs.total', { n: list.data.value.total }) }}</p>
      <UiTable v-if="list.data.value.items.length" :label="$t('docs.tableLabel')">
        <table class="docs" data-testid="team-docs">
          <caption class="visually-hidden">{{ $t('docs.tableLabel') }}</caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('docs.colDoc') }}</th>
              <th scope="col">{{ $t('docs.colStatus') }}</th>
              <th scope="col">{{ $t('docs.colChanged') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in list.data.value.items" :key="it.id" :data-id="it.id">
              <th scope="row" class="doc">
                <RouterLink :to="{ name: 'team-document', params: { id: it.id } }" class="doc__link">
                  <span class="doc__code">{{ it.code ?? $t('docs.noCode') }}</span>
                  <span class="doc__title">{{ it.title }}</span>
                </RouterLink>
                <span class="doc__sub">
                  {{ it.author ? $t('docs.subAuthor', { type: it.type_name, author: it.author, rev: it.revision }) : $t('docs.sub', { type: it.type_name, rev: it.revision }) }}
                </span>
                <span v-if="it.lock" class="doc__lock" :data-mine="it.lock.mine ? 'true' : 'false'">
                  {{ it.lock.mine ? $t('docs.lockMine', { time: formatTime(it.lock.expires_at) }) : $t('docs.lockOther', { holder: it.lock.holder, time: formatTime(it.lock.expires_at) }) }}
                </span>
              </th>
              <td class="status" :data-status="it.status"><UiBadge :tone="tone(it.status)">{{ statusName(it.status) }}</UiBadge></td>
              <td class="date">{{ formatDateTime(it.updated_at) }}</td>
            </tr>
          </tbody>
        </table>
      </UiTable>
      <p v-else class="state" data-testid="team-docs-empty">
        <template v-if="active">{{ $t('docs.nothing') }}</template>
        <template v-else-if="auth.can('write_drafts')">{{ $t('docs.emptyWrite') }}</template>
        <template v-else>{{ $t('docs.empty') }}</template>
      </p>
      <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" :label="$t('docs.pagesLabel')" />
    </template>
    <ImportDocumentDialog v-if="auth.can('write_drafts')" v-model:open="importOpen" />
  </UiSheet>
</template>

<style scoped>
.wide {
  width: 100%;
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.head__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.note {
  color: var(--text-muted);
}
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 0 var(--space-4);
  align-items: end;
  margin-bottom: var(--space-4);
  padding-bottom: var(--space-2);
  border-bottom: 2px solid var(--ink-900);
}
.check,
.actions {
  margin-bottom: var(--space-4);
}
.actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}
.total {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.doc__link {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
}
.doc__code {
  font-family: var(--font-doc);
  font-size: var(--text-sm);
  font-weight: 700;
}
.doc__title {
  font-size: var(--text-md);
  font-weight: 700;
}
.doc__sub,
.doc__lock {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
  font-weight: 400;
}
.doc__lock {
  color: var(--amber-700);
  font-weight: 700;
}
.date {
  white-space: nowrap;
  font-size: var(--text-sm);
}
.state {
  padding: var(--space-5) 0;
  font-size: var(--text-lg);
}
</style>
