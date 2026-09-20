<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import CatalogFilters from '@/components/CatalogFilters.vue'
import PaginationNav from '@/components/PaginationNav.vue'
import SearchBox from '@/components/SearchBox.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { documentsApi } from '@/api/endpoints'
import { isApiError } from '@/api/client'
import type { SearchHit, Snippet } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { useQueryFilters } from '@/composables/useQueryFilters'
import { hasFilters, toApiParams } from '@/lib/catalog'
import { useAuthStore } from '@/stores/auth'
import ErrorView from './ErrorView.vue'

// Поиск по архиву: название, шифр и текст блоков. Запрос и фильтры живут в адресе (/search?q=…&type=memo&page=2).
// Читатель видит только то, что вправе видеть при чтении: закрытое сервер в выдачу не кладёт вовсе.
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const filters = useQueryFilters()

const PER_PAGE = 20
const q = computed(() => (typeof route.query.q === 'string' ? route.query.q.trim() : ''))

// Параметры API: фильтры и страница — теми же именами, что у каталога; сортировки в поиске нет (по совпадению).
const params = computed(() => {
  const p = toApiParams(route.query)
  p.delete('sort')
  p.delete('order')
  p.set('per_page', String(PER_PAGE))
  p.set('q', q.value)
  return p.toString()
})

const result = useQuery({
  queryKey: computed(() => keys.search(params.value)),
  queryFn: ({ signal }) => documentsApi.search(new URLSearchParams(params.value), { signal }),
  enabled: computed(() => q.value.length >= 2),
  placeholderData: keepPreviousData,
  retry: false,
})
const summary = useQuery({ queryKey: keys.summary, queryFn: ({ signal }) => documentsApi.summary({ signal }), staleTime: 60_000 })

const badQuery = computed(() => {
  const e = result.error.value
  return isApiError(e) && e.status === 400 ? (e.fields.q ?? e.fields.type ?? Object.values(e.fields)[0] ?? e.message) : ''
})
const failure = computed(() => {
  const e = result.error.value
  return isApiError(e) && e.status !== 400 ? e : null
})
const tooShort = computed(() => q.value.length > 0 && q.value.length < 2)
const active = computed(() => hasFilters(route.query))

/** Фильтры без запроса и страницы — их SearchBox сохраняет при новом поиске */
const keptFilters = computed(() => {
  const out: Record<string, string> = {}
  for (const k of ['type', 'class', 'dept', 'category', 'containment', 'from', 'to']) {
    const v = route.query[k]
    if (typeof v === 'string' && v) out[k] = v
  }
  return out
})

function resetFilters() {
  void router.push({ query: q.value ? { q: q.value } : {} })
}

const KIND_LABEL: Record<string, string> = { title: 'Название', meta: 'Досье', block: 'Текст' }
const snippetTo = (hit: SearchHit, s: Snippet) => ({
  name: 'document',
  params: { ref: hit.item.slug },
  ...(s.kind === 'block' && s.block_id ? { hash: `#b-${s.block_id}` } : {}),
})
const levelText = (level: number) => (level === 0 ? 'открытый' : `допуск ${level}`)
</script>

<template>
  <div>
    <UiPageHeader title="Поиск" kicker="Центральный архив" />

    <UiSheet wide data-testid="search">
      <SearchBox :initial="q" :keep-filters="keptFilters" />
      <p class="tips">
        Слова запроса ищутся в одном месте — в названии или в одном блоке текста. Морфология учитывается («сотрудник» найдёт «сотрудников»).
        Ищется только по тому, к чему у вас есть допуск.
      </p>

      <CatalogFilters :query="route.query" :summary="summary.data.value" label="Фильтры поиска" @change="(c) => filters.change(c)" @reset="resetFilters" />

      <p v-if="!q" class="state" data-testid="search-empty-query">Введите слово, фразу или шифр документа.</p>
      <p v-else-if="tooShort" class="state">Запрос слишком короткий: нужно хотя бы два знака.</p>
      <p v-else-if="badQuery" class="state state--bad" role="alert" data-testid="search-bad">{{ badQuery }}</p>
      <ErrorView v-else-if="failure" :request-id="failure.requestId" :retrying="result.isFetching.value" @retry="result.refetch()" />
      <UiSkeleton v-else-if="result.isPending.value" :lines="5" label="Ищем…" />

      <template v-else-if="result.data.value">
        <p class="total" role="status" data-testid="search-total">
          <template v-if="result.data.value.ignored">Запрос состоит из слишком частых слов — уточните его.</template>
          <template v-else>Найдено документов: {{ result.data.value.total }}</template>
        </p>

        <ol v-if="result.data.value.items.length" class="hits" :class="{ 'is-stale': result.isPlaceholderData.value }" data-testid="search-hits">
          <li v-for="hit in result.data.value.items" :key="hit.item.code" class="hit" :data-code="hit.item.code">
            <h2 class="hit__title">
              <RouterLink :to="{ name: 'document', params: { ref: hit.item.slug } }">{{ hit.item.code }} — {{ hit.item.title }}</RouterLink>
            </h2>
            <p class="hit__meta">
              {{ hit.item.type_name }} · {{ hit.item.composed.year }} · {{ levelText(hit.item.level) }}
              <UiBadge v-if="auth.user?.directorate && hit.item.status" :tone="hit.item.status === 'published' ? 'published' : 'draft'">{{ hit.item.status }}</UiBadge>
            </p>
            <ul v-if="hit.snippets.length" class="snips">
              <li v-for="(s, i) in hit.snippets" :key="i" class="snip">
                <span class="snip__kind">{{ KIND_LABEL[s.kind] ?? s.kind }}</span>
                <RouterLink class="snip__text" :to="snippetTo(hit, s)">
                  <template v-for="(p, j) in s.parts" :key="j"><mark v-if="p.match">{{ p.text }}</mark><template v-else>{{ p.text }}</template></template>
                </RouterLink>
              </li>
            </ul>
          </li>
        </ol>
        <p v-else-if="!result.data.value.ignored" class="state" data-testid="search-nothing">
          По запросу «{{ result.data.value.query }}» ничего не найдено.
          <template v-if="active">Попробуйте сбросить фильтры.</template>
        </p>

        <PaginationNav :page="result.data.value.page" :pages="result.data.value.pages" />
      </template>
    </UiSheet>
  </div>
</template>

<style scoped>
.tips {
  margin: var(--space-2) 0 var(--space-5);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.total {
  margin: 0 0 var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.state {
  padding: var(--space-4) 0;
  font-size: var(--text-lg);
}
.state--bad {
  color: var(--red-800);
}
.hits {
  margin: 0;
  padding: 0;
  list-style: none;
}
.hits.is-stale {
  opacity: 0.55;
}
.hit {
  padding: var(--space-4) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.hit:last-child {
  border-bottom: 0;
}
.hit__title {
  margin: 0 0 var(--space-1);
  font-family: var(--font-head);
  font-size: var(--text-lg);
  letter-spacing: 0.02em;
  overflow-wrap: anywhere;
}
.hit__meta {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.snips {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-2);
}
.snip {
  display: grid;
  grid-template-columns: 5.5rem 1fr;
  gap: var(--space-3);
  align-items: baseline;
}
.snip__kind {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.snip__text {
  color: inherit;
  text-decoration: none;
  overflow-wrap: anywhere;
}
.snip__text:hover {
  text-decoration: underline;
}
mark {
  padding: 0 0.15em;
  background: #ffe27a;
  color: var(--ink-900);
  border-radius: 2px;
}
@media (max-width: 40rem) {
  .snip {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
