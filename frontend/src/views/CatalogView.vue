<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import CatalogFilters from '@/components/CatalogFilters.vue'
import CatalogCards from '@/components/CatalogCards.vue'
import CatalogFolders from '@/components/CatalogFolders.vue'
import CatalogTable from '@/components/CatalogTable.vue'
import PaginationNav from '@/components/PaginationNav.vue'
import SearchBox from '@/components/SearchBox.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiSelect from '@/ui/UiSelect.vue'
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

const view = computed<'table' | 'folders' | 'cards'>(() => (route.query.view === 'folders' ? 'folders' : route.query.view === 'cards' ? 'cards' : 'table'))
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

function setView(next: 'table' | 'folders' | 'cards') {
  const query = { ...route.query }
  if (next === 'table') delete query.view
  else query.view = next
  void router.push({ query })
}

const filters = useQueryFilters()
const changeFilters = (changes: Record<string, string | null | undefined>) => filters.change(changes)

function resetFilters() {
  const query = { ...route.query }
  for (const k of ['type', 'class', 'dept', 'category', 'containment', 'from', 'to', 'page']) delete query[k]
  void router.push({ query })
}

// Порядок карточек: в реестре его задают заголовки столбцов, у карточек их нет — вместо них список готовых порядков (sort + order в адресе).
const CARD_ORDERS = [
  { value: 'code', label: 'По шифру' },
  { value: 'title', label: 'По названию' },
  { value: 'year:desc', label: 'По году — сначала новые' },
  { value: 'year', label: 'По году — сначала старые' },
  { value: 'class:desc', label: 'По классу опасности — сначала высокий' },
  { value: 'deviation:desc', label: 'По п.о. — сначала больше' },
]
const cardsOrder = computed(() => {
  const sort = typeof route.query.sort === 'string' ? route.query.sort : 'code'
  const key = route.query.order === 'desc' ? `${sort}:desc` : sort
  return CARD_ORDERS.some((o) => o.value === key) ? key : 'code'
})
function setCardsOrder(value: string) {
  const [sort = 'code', order] = value.split(':')
  const query = { ...route.query }
  delete query.page
  if (sort === 'code') delete query.sort
  else query.sort = sort
  if (order) query.order = order
  else delete query.order
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
          <button type="button" class="view" :aria-pressed="view === 'cards' ? 'true' : 'false'" data-testid="view-cards" @click="setView('cards')">Картотека</button>
          <button type="button" class="view" :aria-pressed="view === 'folders' ? 'true' : 'false'" @click="setView('folders')">Папки</button>
        </div>
      </template>
    </UiPageHeader>

    <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />

    <template v-else>
      <CatalogFolders v-if="view === 'folders'" :types="summary.data.value?.types ?? []" @open="openFolder" />

      <UiSheet v-else wide>
        <SearchBox id="catalog-q" class="catalog-search" />
        <p v-if="list.data.value" class="total" role="status">Найдено: {{ list.data.value.total }}</p>
        <CatalogFilters :query="route.query" :summary="summary.data.value" @change="changeFilters" @reset="resetFilters" />

        <UiSkeleton v-if="list.isPending.value" :lines="6" label="Загрузка реестра…" />

        <template v-else-if="list.data.value">
          <div :class="{ 'is-stale': list.isPlaceholderData.value }">
            <template v-if="list.data.value.items.length">
              <div v-if="view === 'cards'" class="cards-order">
                <UiField id="cards-order" label="Порядок карточек">
                  <UiSelect :model-value="cardsOrder" :options="CARD_ORDERS" @update:model-value="setCardsOrder" />
                </UiField>
              </div>
              <CatalogCards v-if="view === 'cards'" :items="list.data.value.items" :show-status="Boolean(auth.user?.directorate)" />
              <CatalogTable v-else :items="list.data.value.items" :show-status="Boolean(auth.user?.directorate)" />
            </template>
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
.catalog-search {
  margin-bottom: var(--space-4);
}
.cards-order {
  max-width: 22rem;
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
