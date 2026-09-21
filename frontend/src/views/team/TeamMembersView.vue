<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import PaginationNav from '@/components/PaginationNav.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTable from '@/ui/UiTable.vue'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { MemberDTO, RoleInfoDTO } from '@/api/generated/httpapi'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { useQueryFilters } from '@/composables/useQueryFilters'
import { formatDate } from '@/lib/format'
import { t } from '@/i18n'
import ErrorView from '../ErrorView.vue'

const route = useRoute()
const router = useRouter()

const one = (v: unknown): string => (Array.isArray(v) ? String(v[0] ?? '') : typeof v === 'string' ? v : '')
const q = computed(() => one(route.query.q))
const role = computed(() => one(route.query.role))
const staff = computed(() => one(route.query.staff) === '1')
const page = computed(() => Math.max(1, Number.parseInt(one(route.query.page), 10) || 1))

const roleList = useQuery({ queryKey: keys.teamRoles, queryFn: ({ signal }) => teamApi.roles({ signal }) })
// Состояние списка живёт в адресе (/team/members?q=vera&role=editor&staff=1&page=2).
const list = useQuery({
  queryKey: computed(() => keys.teamMembers(`${q.value}|${role.value}|${staff.value}|${page.value}`)),
  queryFn: ({ signal }) => teamApi.members({ q: q.value, role: role.value, staff: staff.value, page: page.value }, { signal }),
  placeholderData: keepPreviousData,
})

// Строки — копии ответа: после выдачи или снятия роли строка обновляется ответом сервера, без перезагрузки списка.
const members = ref<MemberDTO[]>([])
watch(
  () => list.data.value,
  (d) => {
    members.value = d ? d.members.map((m) => ({ ...m })) : []
  },
  { immediate: true },
)

const roles = computed<RoleInfoDTO[]>(() => roleList.data.value?.roles ?? [])
const roleOptions = computed(() => roles.value.map((r) => ({ value: r.id, label: r.name })))
const search = ref(q.value)
watch(q, (v) => (search.value = v))

const filters = useQueryFilters()
const setFilters = (changes: Record<string, string>) => filters.change(changes)
const submitSearch = () => setFilters({ q: search.value.trim() })
const reset = () => router.push({ query: {} })

const busy = ref('') // «логин:роль» — для какой кнопки идёт запрос
const status = ref('') // объявляется скринридерам после успеха
const problem = ref('')

const hasRole = (m: MemberDTO, r: RoleInfoDTO) => m.roles.some((x) => x.id === r.id)

async function toggle(m: MemberDTO, r: RoleInfoDTO) {
  const grant = !hasRole(m, r)
  busy.value = `${m.login}:${r.id}`
  problem.value = ''
  status.value = ''
  try {
    const res = grant ? await teamApi.grant(m.login, r.id) : await teamApi.revoke(m.login, r.id)
    Object.assign(m, res.member)
    status.value = t(grant ? 'members.granted' : 'members.revoked', { role: r.name, login: m.login })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    problem.value = describeApiError(err)
  } finally {
    busy.value = ''
  }
}

const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
</script>

<template>
  <UiSheet as="section" wide aria-labelledby="members-title" class="wide">
    <h2 id="members-title">{{ $t('members.title') }}</h2>
    <p class="note">{{ $t('members.note') }}</p>

    <form class="filters" :aria-label="$t('members.searchLabel')" @submit.prevent="submitSearch">
      <UiField id="t-q" :label="$t('members.login')">
        <UiInput v-model="search" type="search" :maxlength="24" :placeholder="$t('members.loginPlaceholder')" @change="submitSearch" />
      </UiField>
      <UiField id="t-role" :label="$t('members.role')">
        <UiSelect :model-value="role" :options="roleOptions" :placeholder="$t('members.anyRole')" @update:model-value="setFilters({ role: $event })" />
      </UiField>
      <div class="check">
        <UiCheckbox :model-value="staff" :label="$t('members.onlyStaff')" @update:model-value="setFilters({ staff: $event ? '1' : '' })" />
      </div>
      <div class="actions">
        <UiButton type="submit" variant="primary">{{ $t('members.find') }}</UiButton>
        <UiButton v-if="q || role || staff" variant="link" @click="reset">{{ $t('members.reset') }}</UiButton>
      </div>
    </form>

    <p class="visually-hidden" role="status">{{ status }}</p>
    <UiAlert v-if="problem" tone="danger">{{ problem }}</UiAlert>

    <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
    <UiSkeleton v-else-if="list.isPending.value" :lines="5" />

    <template v-else-if="list.data.value">
      <p class="total" data-testid="members-total">{{ $t('members.total', { n: list.data.value.total }) }}</p>
      <UiTable v-if="members.length" :label="$t('members.tableLabel')">
        <table class="members" data-testid="members">
          <caption class="visually-hidden">{{ $t('members.tableLabel') }}</caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('members.colUser') }}</th>
              <th scope="col">{{ $t('members.colRoles') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in members" :key="m.login" :data-login="m.login">
              <th scope="row" class="who">
                <span class="who__login">{{ m.login }}</span>
                <span class="who__sub">{{ $t('members.sub', { level: m.level_name, when: formatDate(m.created_at) }) }}</span>
              </th>
              <td>
                <p v-if="m.directorate" class="dir">{{ $t('members.directorate') }}</p>
                <div v-else class="toggles" role="group" :aria-label="$t('members.rolesOf', { login: m.login })">
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
      </UiTable>
      <p v-else class="state" data-testid="members-empty">{{ $t('members.empty') }}</p>
      <PaginationNav :page="list.data.value.page" :pages="list.data.value.pages" />
    </template>
  </UiSheet>
</template>

<style scoped>
.wide {
  width: 100%;
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
.check {
  margin-bottom: var(--space-4);
}
.actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.total {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.who__login {
  display: block;
  font-family: var(--font-head);
  font-size: var(--text-lg);
  letter-spacing: 0.05em;
}
.who__sub {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.dir {
  margin: 0;
  font-weight: 700;
}
.toggles {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.toggle {
  min-height: var(--control-h); /* цель нажатия не меньше 44 px */
  padding: 0 var(--space-4);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: transparent;
  cursor: pointer;
}
.toggle:hover:not(:disabled) {
  border-color: var(--ink-900);
}
.toggle[aria-pressed='true'] {
  border-color: var(--ink-900);
  background: var(--ink-900);
  color: var(--paper-50);
  font-weight: 700;
}
.toggle:disabled {
  opacity: 0.6;
  cursor: progress;
}
.state {
  padding: var(--space-5) 0;
  font-size: var(--text-lg);
}
</style>
