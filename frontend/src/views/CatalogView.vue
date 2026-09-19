<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/index.js'
import CatalogFilters from '../components/CatalogFilters.vue'
import CatalogFolders from '../components/CatalogFolders.vue'
import CatalogTable from '../components/CatalogTable.vue'
import PaginationNav from '../components/PaginationNav.vue'
import { useQueryFilters } from '../composables/useQueryFilters.js'
import { useResource } from '../composables/useResource.js'
import { hasFilters, toApiParams, withFilters } from '../lib/catalog.js'
import { useAuthStore } from '../stores/auth.js'
import ErrorView from './ErrorView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const view = computed(() => (route.query.view === 'folders' ? 'folders' : 'table'))
const params = computed(() => toApiParams(route.query).toString())

// Каталог перезагружается при любом изменении фильтров, сортировки или страницы; устаревший запрос отменяется.
const list = useResource((signal) => api.get(`/documents?${params.value}`, { signal }), () => params.value)
// Сводка нужна фильтрам и «папкам»; не зависит от выбранных фильтров.
const summary = useResource((signal) => api.get('/documents/summary', { signal }))

const filtered = computed(() => hasFilters(route.query))

function setView(next) {
  const query = { ...route.query }
  if (next === 'folders') query.view = 'folders'
  else delete query.view
  router.push({ query })
}

const filters = useQueryFilters()
const changeFilters = (changes) => filters.change(changes)

function resetFilters() {
  const query = { ...route.query }
  for (const k of ['type', 'class', 'dept', 'category', 'containment', 'from', 'to', 'page']) delete query[k]
  router.push({ query })
}

function openFolder(type) {
  const query = withFilters(route.query, { type })
  delete query.view
  router.push({ query })
}
</script>

<template>
  <article>
    <h1>Каталог</h1>

    <div class="toolbar">
      <div class="views" role="group" aria-label="Вид каталога">
        <button type="button" class="view" :aria-pressed="view === 'table' ? 'true' : 'false'" @click="setView('table')">Реестр</button>
        <button type="button" class="view" :aria-pressed="view === 'folders' ? 'true' : 'false'" @click="setView('folders')">Папки</button>
      </div>
      <p v-if="list.data.value && view === 'table'" class="total" role="status">Найдено: {{ list.data.value.total }}</p>
    </div>

    <ErrorView v-if="list.error.value" :request-id="list.error.value.requestId" :retrying="list.loading.value" @retry="list.reload" />

    <template v-else>
      <CatalogFolders
        v-if="view === 'folders'"
        :types="summary.data.value?.types ?? []"
        @open="openFolder"
      />

      <template v-else>
        <CatalogFilters :query="route.query" :summary="summary.data.value" @change="changeFilters" @reset="resetFilters" />

        <p v-if="list.loading.value && !list.data.value" class="state" role="status">Загрузка реестра…</p>

        <template v-else-if="list.data.value">
          <CatalogTable
            v-if="list.data.value.items.length"
            :items="list.data.value.items"
            :show-status="Boolean(auth.user?.directorate)"
          />
          <p v-else class="state" data-testid="catalog-empty">
            <template v-if="filtered">По заданным условиям ничего не найдено. <button type="button" class="form-link" @click="resetFilters">Сбросить фильтры</button></template>
            <template v-else>В архиве пока нет документов, доступных вам.</template>
          </p>
          <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" />
        </template>
      </template>
    </template>
  </article>
</template>

<style scoped>
.toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-4); }
.views { display: inline-flex; border: 2px solid var(--ink); }
.view {
  padding: 0.4rem 1.1rem;
  border: 0;
  background: transparent;
  color: var(--ink);
  font-family: var(--font-head);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  cursor: pointer;
}
.view + .view { border-left: 2px solid var(--ink); }
.view[aria-pressed='true'] { background: var(--ink); color: var(--paper); }
.total { margin: 0; color: var(--ink-soft); }
.state { color: var(--ink-soft); }
</style>
