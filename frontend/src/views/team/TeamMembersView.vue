<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api/index.js'
import { ApiError } from '../../api/client.js'
import PaginationNav from '../../components/PaginationNav.vue'
import { describeApiError } from '../../composables/useForm.js'
import { useResource } from '../../composables/useResource.js'
import { useQueryFilters } from '../../composables/useQueryFilters.js'
import { formatDate } from '../../lib/format.js'
import ErrorView from '../ErrorView.vue'

const route = useRoute()
const router = useRouter()

const one = (v) => (Array.isArray(v) ? v[0] : v) || ''
const q = computed(() => one(route.query.q))
const role = computed(() => one(route.query.role))
const staff = computed(() => one(route.query.staff) === '1')

// Состояние списка живёт в адресе (/team/members?q=vera&role=editor&staff=1&page=2).
const params = computed(() => {
  const p = new URLSearchParams()
  if (q.value) p.set('q', q.value)
  if (role.value) p.set('role', role.value)
  if (staff.value) p.set('staff', '1')
  const page = Number.parseInt(one(route.query.page), 10)
  if (page > 1) p.set('page', String(page))
  return p.toString()
})

const roleList = useResource((signal) => api.get('/team/roles', { signal }))
const list = useResource(
  (signal) => api.get(params.value ? `/team/members?${params.value}` : '/team/members', { signal }),
  () => params.value,
)

// Строки — копии ответа: после выдачи или снятия роли строка обновляется ответом сервера, без перезагрузки списка.
const members = ref([])
watch(
  () => list.data.value,
  (d) => {
    members.value = d ? d.members.map((m) => ({ ...m })) : []
  },
  { immediate: true },
)

const roles = computed(() => roleList.data.value?.roles ?? [])
const search = ref(q.value)
watch(q, (v) => (search.value = v))

const filters = useQueryFilters()
const setFilters = (changes) => filters.change(changes)
function submitSearch() {
  setFilters({ q: search.value.trim() })
}
function reset() {
  router.push({ query: {} })
}

const busy = ref('') // «логин:роль» — для какой кнопки идёт запрос
const status = ref('') // объявляется скринридерам после успеха
const problem = ref('')

const hasRole = (m, r) => m.roles.some((x) => x.id === r.id)

async function toggle(m, r) {
  const grant = !hasRole(m, r)
  const url = `/team/members/${encodeURIComponent(m.login)}/roles/${r.id}`
  busy.value = `${m.login}:${r.id}`
  problem.value = ''
  status.value = ''
  try {
    const res = grant ? await api.put(url) : await api.delete(url)
    Object.assign(m, res.member)
    status.value = grant ? `Роль «${r.name}» выдана: ${m.login}.` : `Роль «${r.name}» снята: ${m.login}.`
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    problem.value = describeApiError(err)
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <section aria-labelledby="members-title">
    <h2 id="members-title">Команда</h2>
    <p class="note">Выдайте пользователю роль — он получит её сразу, без повторного входа. Директорат подразумевает все роли.</p>

    <form class="filters" aria-label="Поиск пользователей" @submit.prevent="submitSearch">
      <div class="field">
        <label for="t-q">Логин</label>
        <input id="t-q" v-model="search" type="search" maxlength="24" autocomplete="off" placeholder="часть логина" @change="submitSearch">
      </div>
      <div class="field">
        <label for="t-role">Роль</label>
        <select id="t-role" :value="role" @change="setFilters({ role: $event.target.value })">
          <option value="">Любая</option>
          <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
      </div>
      <div class="field field--check">
        <label class="check"><input type="checkbox" :checked="staff" @change="setFilters({ staff: $event.target.checked ? '1' : '' })"> Только команда</label>
      </div>
      <div class="field field--actions">
        <button type="submit" class="btn">Найти</button>
        <button v-if="q || role || staff" type="button" class="form-link" @click="reset">Сбросить</button>
      </div>
    </form>

    <p class="visually-hidden" role="status">{{ status }}</p>
    <p v-if="problem" class="form-error" role="alert">{{ problem }}</p>

    <ErrorView v-if="list.error.value" :request-id="list.error.value.requestId" :retrying="list.loading.value" @retry="list.reload" />
    <p v-else-if="list.loading.value && !list.data.value" class="state" role="status">Загрузка…</p>

    <template v-else-if="list.data.value">
      <p class="total" data-testid="members-total">Найдено: {{ list.data.value.total }}</p>
      <div v-if="members.length" class="wrap" tabindex="0" role="region" aria-label="Пользователи и их роли">
        <table class="members" data-testid="members">
          <caption class="visually-hidden">Пользователи и их роли</caption>
          <thead>
            <tr>
              <th scope="col">Пользователь</th>
              <th scope="col">Роли</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in members" :key="m.login" :data-login="m.login">
              <th scope="row" class="who">
                <span class="login">{{ m.login }}</span>
                <span class="sub">{{ m.level_name }} · принят {{ formatDate(m.created_at) }}</span>
              </th>
              <td>
                <p v-if="m.directorate" class="dir">Директорат: все роли и права</p>
                <div v-else class="toggles" role="group" :aria-label="`Роли пользователя ${m.login}`">
                  <button
                    v-for="r in roles"
                    :key="r.id"
                    type="button"
                    class="toggle"
                    :aria-pressed="hasRole(m, r) ? 'true' : 'false'"
                    :data-role="r.id"
                    :disabled="busy === `${m.login}:${r.id}`"
                    @click="toggle(m, r)"
                  >
                    {{ r.name }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="state" data-testid="members-empty">Никого не найдено.</p>
      <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" />
    </template>
  </section>
</template>

<style scoped>
.note, .state, .total { color: var(--ink-soft); }
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
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
.members { width: 100%; border-collapse: collapse; }
.members th, .members td { padding: 0.5rem 0.7rem; border-bottom: 1px solid var(--rule); text-align: left; vertical-align: middle; }
.members thead th { border-bottom: 2px solid var(--ink); background: var(--paper-shade); font-family: var(--font-head); letter-spacing: 0.06em; text-transform: uppercase; white-space: nowrap; }
.who { width: 40%; min-width: 9rem; }
.login { display: block; font-weight: 700; overflow-wrap: anywhere; }
.sub { display: block; font-size: 0.8rem; font-weight: 400; color: var(--ink-soft); }
.dir { margin: 0; font-style: italic; }
.toggles { display: flex; flex-wrap: wrap; gap: var(--space-1); }
@media (max-width: 34rem) {
  .toggles { display: grid; grid-template-columns: 1fr 1fr; }
  .toggle { padding-inline: 0.4rem; }
}
.toggle {
  min-height: 2.75rem;
  padding: 0.3rem 0.8rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink);
  font: inherit;
  cursor: pointer;
}
.toggle:hover { background: var(--paper-shade); }
.toggle[aria-pressed='true'] { background: var(--ink); color: var(--paper); font-weight: 700; }
.toggle:disabled { opacity: 0.6; cursor: progress; }
</style>
