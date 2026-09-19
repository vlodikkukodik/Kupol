<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTable from '@/ui/UiTable.vue'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { useAuthStore, type Capability } from '@/stores/auth'
import ErrorView from '../ErrorView.vue'

const auth = useAuthStore()
const query = useQuery({ queryKey: keys.teamRoles, queryFn: ({ signal }) => teamApi.roles({ signal }) })
const data = computed(() => query.data.value)
const requestId = computed(() => (isApiError(query.error.value) ? query.error.value.requestId : ''))

const mine = computed(() => {
  const u = auth.user
  if (!u) return []
  return u.directorate ? [{ id: 'directorate', name: 'Директорат' }] : u.roles
})
const has = (capability: string) => auth.can(capability as Capability)
</script>

<template>
  <ErrorView v-if="query.isError.value" :request-id="requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiSheet v-else-if="!data"><UiSkeleton :lines="5" /></UiSheet>

  <div v-else class="stack">
    <UiSheet as="section" aria-labelledby="mine-title">
      <h2 id="mine-title">Ваши роли</h2>
      <p v-if="mine.length" data-testid="my-roles">
        <template v-for="(r, i) in mine" :key="r.id"><strong>{{ r.name }}</strong><template v-if="i < mine.length - 1">, </template></template>
      </p>
      <p v-if="auth.user?.directorate">Директорат подразумевает все роли и все права. Роли остальным выдаются на экране «Команда».</p>

      <h3>Что вам разрешено</h3>
      <ul class="rights" data-testid="my-rights">
        <li v-for="c in data.capabilities" :key="c.id" :class="{ off: !has(c.id) }">
          <span class="mark" aria-hidden="true">{{ has(c.id) ? '✓' : '—' }}</span>
          <span class="visually-hidden">{{ has(c.id) ? 'Разрешено:' : 'Не разрешено:' }}</span>
          {{ c.name }}
        </li>
      </ul>
    </UiSheet>

    <UiSheet as="section" aria-labelledby="roles-title">
      <h2 id="roles-title">Что даёт каждая роль</h2>
      <p class="note">Ролей у человека может быть несколько, права складываются. Роли выдаёт Директорат.</p>
      <UiTable label="Таблица ролей и прав">
        <table class="matrix" data-testid="roles-matrix">
          <caption class="visually-hidden">Права по ролям</caption>
          <thead>
            <tr>
              <th scope="col">Право</th>
              <th v-for="r in data.roles" :key="r.id" scope="col">{{ r.name }}</th>
              <th scope="col">{{ data.directorate.name }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in data.capabilities" :key="c.id">
              <th scope="row">{{ c.name }}</th>
              <td v-for="r in data.roles" :key="r.id" class="cell">
                <template v-if="r.capabilities.some((x) => x.id === c.id)">
                  <span aria-hidden="true">✓</span><span class="visually-hidden">да</span>
                </template>
                <template v-else><span aria-hidden="true" class="no">—</span><span class="visually-hidden">нет</span></template>
              </td>
              <td class="cell"><span aria-hidden="true">✓</span><span class="visually-hidden">да</span></td>
            </tr>
          </tbody>
        </table>
      </UiTable>
    </UiSheet>
  </div>
</template>

<style scoped>
.stack {
  display: grid;
  grid-template-columns: minmax(0, 1fr); /* без этого широкая таблица растягивает колонку и страницу */
  gap: var(--space-5);
}
.stack > * {
  max-width: none;
  margin: 0;
  width: 100%;
}
.note {
  color: var(--text-muted);
}
.rights {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(18rem, 1fr));
  gap: var(--space-1) var(--space-5);
  margin: 0;
  padding: 0;
  list-style: none;
}
.rights li {
  padding: var(--space-1) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.rights li.off {
  color: var(--text-muted);
}
.mark {
  display: inline-block;
  width: 1.4em;
  font-weight: 700;
}
.matrix .cell {
  text-align: center;
  font-weight: 700;
}
.matrix .no {
  color: var(--text-muted);
}
</style>
