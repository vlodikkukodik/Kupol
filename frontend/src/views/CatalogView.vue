<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import CatalogFilters from '@/components/CatalogFilters.vue'
import CatalogFolders from '@/components/CatalogFolders.vue'
import CatalogTable from '@/components/CatalogTable.vue'
import PaginationNav from '@/components/PaginationNav.vue'
import UiButton from '@/ui/UiButton.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { documentsApi } from '@/api/endpoints'
import { isApiError } from '@/api/client'
import { keys } from '@/api/query'
import { useQueryFilters } from '@/composables/useQueryFilters'
import { hasFilters, toApiParams, withFilters } from '@/lib/catalog'
import { useAuthStore } from '@/stores/auth'
import ErrorView from './ErrorView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const view = computed(() => (route.query.view === 'folders' ? 'folders' : 'table'))
const params = computed(() => toApiParams(route.query).toString())

// Каталог перезагружается при любом изменении фильтров, сортировки или страницы; устаревший запрос отменяется,
// а пока грузится новая страница — остаётся предыдущая (без мигания «Загрузка»).
const list = useQuery({
  queryKey: computed(() => keys.list(params.value)),
  queryFn: ({ signal }) => documentsApi.list(new URLSearchParams(params.value), { signal }),
  placeholderData: keepPreviousData,
})
// Сводка нужна фильтрам и «папкам»; не зависит от выбранных фильтров.
const summary = useQuery({ queryKey: keys.summary, queryFn: ({ signal }) => documentsApi.summary({ signal }), staleTime: 60_000 })

const filtered = computed(() => hasFilters(route.query))
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

function setView(next: 'table' | 'folders') {
  const query = { ...route.query }
  if (next === 'folders') query.view = 'folders'
  else delete query.view
  void router.push({ query })
}

const filters = useQueryFilters()
const changeFilters = (changes: Record<string, string | null | undefined>) => filters.change(changes)

function resetFilters() {
  const query = { ...route.query }
  for (const k of ['type', 'class', 'dept', 'category', 'containment', 'from', 'to', 'page']) delete query[k]
  void router.push({ query })
}

function openFolder(type: string) {
  const query = withFilters(route.query, { type })
  delete query.view
  void router.push({ query })
}
</script>

<template>
  <div>
    <UiPageHeader title="Каталог" kicker="Реестр Центрального архива">
      <template #actions>
        <div class="views" role="group" aria-label="Вид каталога">
          <button type="button" class="view" :aria-pressed="view === 'table' ? 'true' : 'false'" @click="setView('table')">Реестр</button>
          <button type="button" class="view" :aria-pressed="view === 'folders' ? 'true' : 'false'" @click="setView('folders')">Папки</button>
        </div>
      </template>
    </UiPageHeader>

    <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />

    <template v-else>
      <CatalogFolders v-if="view === 'folders'" :types="summary.data.value?.types ?? []" @open="openFolder" />

      <UiSheet v-else wide>
        <p v-if="list.data.value" class="total" role="status">Найдено: {{ list.data.value.total }}</p>
        <CatalogFilters :query="route.query" :summary="summary.data.value" @change="changeFilters" @reset="resetFilters" />

        <UiSkeleton v-if="list.isPending.value" :lines="6" label="Загрузка реестра…" />

        <template v-else-if="list.data.value">
          <div :class="{ 'is-stale': list.isPlaceholderData.value }">
            <CatalogTable v-if="list.data.value.items.length" :items="list.data.value.items" :show-status="Boolean(auth.user?.directorate)" />
            <p v-else class="state" data-testid="catalog-empty">
              <template v-if="filtered">
                По заданным условиям ничего не найдено.
                <UiButton variant="link" @click="resetFilters">Сбросить фильтры</UiButton>
              </template>
              <template v-else>В архиве пока нет документов, доступных вам.</template>
            </p>
          </div>
          <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" />
        </template>
      </UiSheet>
    </template>
  </div>
</template>

<style scoped>
.views {
  display: inline-flex;
  border: 2px solid var(--on-bg);
  border-radius: var(--radius-2);
  overflow: hidden;
}
.view {
  min-height: var(--control-h);
  padding: 0 var(--space-5);
  border: 0;
  background: transparent;
  color: var(--on-bg);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.view + .view {
  border-left: 2px solid var(--on-bg);
}
.view:hover {
  background: rgb(255 255 255 / 0.1);
}
.view[aria-pressed='true'] {
  background: var(--paper-100);
  color: var(--ink-900);
  font-weight: 700;
}
.total {
  margin: 0 0 var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.state {
  padding: var(--space-5) 0;
  font-size: var(--text-lg);
}
.is-stale {
  opacity: 0.55;
  transition: opacity var(--dur-fast) var(--ease);
}
</style>
