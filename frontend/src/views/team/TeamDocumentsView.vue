<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api/index.js'
import PaginationNav from '../../components/PaginationNav.vue'
import { useDocumentMeta } from '../../composables/useDocumentMeta.js'
import { useQueryFilters } from '../../composables/useQueryFilters.js'
import { useResource } from '../../composables/useResource.js'
import { formatDateTime, formatTime } from '../../lib/format.js'
import { useAuthStore } from '../../stores/auth.js'
import ErrorView from '../ErrorView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { statuses, types, statusName } = useDocumentMeta()

const one = (v) => (Array.isArray(v) ? v[0] : v) || ''
const status = computed(() => one(route.query.status))
const type = computed(() => one(route.query.type))
const q = computed(() => one(route.query.q))
const mine = computed(() => one(route.query.mine) === '1')

// Состояние списка — в адресе: /team/documents?status=review&mine=1&page=2
const params = computed(() => {
  const p = new URLSearchParams()
  if (status.value) p.set('status', status.value)
  if (type.value) p.set('type', type.value)
  if (q.value) p.set('q', q.value)
  if (mine.value) p.set('mine', '1')
  const page = Number.parseInt(one(route.query.page), 10)
  if (page > 1) p.set('page', String(page))
  p.set('per_page', '25')
  return p.toString()
})
const list = useResource((signal) => api.get(`/team/documents?${params.value}`, { signal }), () => params.value)

const filters = useQueryFilters()
const search = ref(q.value)
watch(q, (v) => (search.value = v))
const active = computed(() => Boolean(status.value || type.value || q.value || mine.value))
function reset() {
  router.push({ query: {} })
}
</script>

<template>
  <section aria-labelledby="docs-title">
    <div class="head">
      <h2 id="docs-title">Документы</h2>
      <RouterLink v-if="auth.can('write_drafts')" class="btn" :to="{ name: 'team-document-new' }">Новый документ</RouterLink>
    </div>
    <p class="note">
      Здесь ваши документы в любом статусе<template v-if="auth.can('review') || auth.can('edit_published')">
        и все документы, вышедшие из черновика</template>. Чужие черновики не видны никому, кроме Директората.
    </p>

    <form class="filters" aria-label="Отбор документов" @submit.prevent="filters.change({ q: search.trim() })">
      <div class="field">
        <label for="d-q">Название или шифр</label>
        <input id="d-q" v-model="search" type="search" maxlength="100" autocomplete="off" @change="filters.change({ q: search.trim() })">
      </div>
      <div class="field">
        <label for="d-status">Статус</label>
        <select id="d-status" :value="status" @change="filters.change({ status: $event.target.value })">
          <option value="">Любой</option>
          <option v-for="s in statuses" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
      </div>
      <div class="field">
        <label for="d-type">Тип</label>
        <select id="d-type" :value="type" @change="filters.change({ type: $event.target.value })">
          <option value="">Любой</option>
          <option v-for="t in types" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>
      <div class="field field--check">
        <label class="check"><input type="checkbox" :checked="mine" @change="filters.change({ mine: $event.target.checked ? '1' : '' })"> Только мои</label>
      </div>
      <div class="field field--actions">
        <button type="submit" class="btn">Найти</button>
        <button v-if="active" type="button" class="form-link" @click="reset">Сбросить</button>
      </div>
    </form>

    <ErrorView v-if="list.error.value" :request-id="list.error.value.requestId" :retrying="list.loading.value" @retry="list.reload" />
    <p v-else-if="list.loading.value && !list.data.value" class="state" role="status">Загрузка…</p>

    <template v-else-if="list.data.value">
      <p class="total" role="status" data-testid="team-docs-total">Найдено: {{ list.data.value.total }}</p>
      <div v-if="list.data.value.items.length" class="wrap" tabindex="0" role="region" aria-label="Документы команды">
        <table class="docs" data-testid="team-docs">
          <caption class="visually-hidden">Документы команды</caption>
          <thead>
            <tr>
              <th scope="col">Документ</th>
              <th scope="col">Статус</th>
              <th scope="col">Изменён</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in list.data.value.items" :key="it.id" :data-id="it.id">
              <th scope="row" class="doc">
                <RouterLink :to="{ name: 'team-document', params: { id: it.id } }">
                  <span class="code">{{ it.code ?? 'без шифра' }}</span>
                  <span class="title">{{ it.title }}</span>
                </RouterLink>
                <span class="sub">
                  {{ it.type_name }}<template v-if="it.author"> · автор {{ it.author }}</template> · редакция {{ it.revision }}
                </span>
                <span v-if="it.lock" class="lock" :data-mine="it.lock.mine ? 'true' : 'false'">
                  {{ it.lock.mine ? 'В работе у вас' : `Редактирует: ${it.lock.holder}` }} до {{ formatTime(it.lock.expires_at) }}
                </span>
              </th>
              <td class="status" :data-status="it.status">{{ statusName(it.status) }}</td>
              <td class="date">{{ formatDateTime(it.updated_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="state" data-testid="team-docs-empty">
        <template v-if="active">По заданным условиям ничего не найдено.</template>
        <template v-else-if="auth.can('write_drafts')">Документов пока нет. Начните с кнопки «Новый документ».</template>
        <template v-else>Документов, доступных вам, пока нет.</template>
      </p>
      <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" />
    </template>
  </section>
</template>

<style scoped>
.head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); }
.note, .state, .total { color: var(--ink-soft); }
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: var(--space-3);
  align-items: end;
  margin-bottom: var(--space-4);
  padding: var(--space-3);
  border: 1px solid var(--ink);
  background: var(--paper-shade);
}
label {
  display: block;
  margin-bottom: 0.2rem;
  font-family: var(--font-head);
  font-size: 0.85rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
input[type='search'], select {
  width: 100%;
  min-width: 0;
  padding: 0.4rem 0.5rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: #f4eedc;
  color: var(--ink);
  font: inherit;
}
.check { display: flex; align-items: center; gap: var(--space-2); margin: 0; letter-spacing: 0.06em; cursor: pointer; }
.check input { width: 1.2rem; height: 1.2rem; }
.field--actions { display: flex; align-items: center; gap: var(--space-3); }
.wrap { overflow-x: auto; }
.docs { width: 100%; border-collapse: collapse; }
.docs th, .docs td { padding: 0.55rem 0.7rem; border-bottom: 1px solid var(--rule); text-align: left; vertical-align: top; }
.docs thead th { border-bottom: 2px solid var(--ink); background: var(--paper-shade); font-family: var(--font-head); letter-spacing: 0.06em; text-transform: uppercase; white-space: nowrap; }
.doc { font-weight: 400; min-width: 12rem; }
.doc a { color: var(--ink); }
.doc a:hover { color: var(--stamp-red); }
.code { display: block; font-weight: 700; }
.title { overflow-wrap: anywhere; }
.sub, .lock { display: block; font-size: 0.8rem; color: var(--ink-soft); }
.lock { margin-top: 0.2rem; font-weight: 700; color: var(--stamp-red); }
.lock[data-mine='true'] { color: var(--ink); }
.status { white-space: nowrap; }
.date { white-space: nowrap; font-size: 0.85rem; color: var(--ink-soft); }
@media (max-width: 34rem) {
  .date { white-space: normal; }
}
</style>
