<script setup>
import { computed } from 'vue'
import { api } from '../../api/index.js'
import { useResource } from '../../composables/useResource.js'
import { useAuthStore } from '../../stores/auth.js'
import ErrorView from '../ErrorView.vue'

const auth = useAuthStore()
const { data, error, loading, reload } = useResource((signal) => api.get('/team/roles', { signal }))

const mine = computed(() => {
  const u = auth.user
  if (!u) return []
  return u.directorate ? [{ id: 'directorate', name: 'Директорат' }] : u.roles
})
const has = (capability) => auth.can(capability)
</script>

<template>
  <ErrorView v-if="error" :request-id="error.requestId" :retrying="loading" @retry="reload" />
  <p v-else-if="!data" class="state" role="status">Загрузка…</p>

  <template v-else>
    <section aria-labelledby="mine-title">
      <h2 id="mine-title">Ваши роли</h2>
      <p v-if="mine.length" data-testid="my-roles">
        <template v-for="(r, i) in mine" :key="r.id"><strong>{{ r.name }}</strong><template v-if="i < mine.length - 1">, </template></template>
      </p>
      <p v-if="auth.user?.directorate">Директорат подразумевает все роли и все права. Роли остальным выдаются на экране «Команда».</p>

      <h3 class="sub">Что вам разрешено</h3>
      <ul class="rights" data-testid="my-rights">
        <li v-for="c in data.capabilities" :key="c.id" :class="{ off: !has(c.id) }">
          <span class="mark" aria-hidden="true">{{ has(c.id) ? '✓' : '—' }}</span>
          <span class="visually-hidden">{{ has(c.id) ? 'Разрешено:' : 'Не разрешено:' }}</span>
          {{ c.name }}
        </li>
      </ul>
    </section>

    <section aria-labelledby="roles-title">
      <h2 id="roles-title">Что даёт каждая роль</h2>
      <p class="note">Ролей у человека может быть несколько, права складываются. Роли выдаёт Директорат.</p>
      <div class="wrap" tabindex="0" role="region" aria-label="Таблица ролей и прав">
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
              <td v-for="r in data.roles" :key="r.id">
                <template v-if="r.capabilities.some((x) => x.id === c.id)">
                  <span aria-hidden="true">✓</span><span class="visually-hidden">да</span>
                </template>
                <template v-else><span aria-hidden="true">—</span><span class="visually-hidden">нет</span></template>
              </td>
              <td><span aria-hidden="true">✓</span><span class="visually-hidden">да</span></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>
</template>

<style scoped>
section { margin-bottom: var(--space-5); }
.state, .note { color: var(--ink-soft); }
.sub { margin: var(--space-3) 0 var(--space-2); font-size: 1.05rem; letter-spacing: 0.1em; }
.rights { margin: 0; padding: 0; list-style: none; }
.rights li { padding: 0.35rem 0; border-bottom: 1px solid var(--rule); }
.rights li.off { color: var(--ink-soft); }
.mark { display: inline-block; width: 1.6rem; font-weight: 700; }
.wrap { overflow-x: auto; }
.matrix { width: 100%; border-collapse: collapse; }
.matrix th, .matrix td { padding: 0.5rem 0.7rem; border-bottom: 1px solid var(--rule); text-align: center; }
.matrix thead th { border-bottom: 2px solid var(--ink); background: var(--paper-shade); font-family: var(--font-head); letter-spacing: 0.06em; text-transform: uppercase; }
.matrix tbody th { text-align: left; font-weight: 400; }
</style>
