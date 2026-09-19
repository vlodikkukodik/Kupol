<script setup>
import { api } from '../api/index.js'
import { useResource } from '../composables/useResource.js'
import { formatComposed } from '../lib/format.js'

// «Поступило в ЦАК»: последние опубликованные документы, доступные читателю по его допуску.
const { data, error, loading } = useResource((signal) => api.get('/documents/recent?limit=8', { signal }))
</script>

<template>
  <section class="feed" aria-labelledby="feed-title">
    <h2 id="feed-title">Поступило в ЦАК</h2>

    <p v-if="loading && !data" class="state" role="status">Загрузка…</p>
    <p v-else-if="error" class="state">Лента временно недоступна.</p>
    <template v-else-if="data">
      <ul v-if="data.items.length" class="items" data-testid="recent-feed">
        <li v-for="it in data.items" :key="it.slug">
          <RouterLink :to="{ name: 'document', params: { ref: it.slug } }">
            <span class="code">{{ it.code }}</span>
            <span class="title">{{ it.title }}</span>
            <span class="meta">{{ it.type_name }}, {{ formatComposed(it.composed) }}</span>
          </RouterLink>
        </li>
      </ul>
      <p v-else class="state" data-testid="recent-empty">Пока ничего не поступило.</p>
      <p class="all"><RouterLink to="/catalog">Весь каталог →</RouterLink></p>
    </template>
  </section>
</template>

<style scoped>
.feed { margin-top: var(--space-5); }
.items { margin: 0 0 var(--space-3); padding: 0; list-style: none; border-top: 2px solid var(--ink); }
.items li { border-bottom: 1px solid var(--rule); }
.items a { display: grid; grid-template-columns: 9rem 1fr; gap: 0 var(--space-3); padding: 0.55rem 0.2rem; color: var(--ink); text-decoration: none; }
.items a:hover { background: var(--paper-shade); }
.code { font-weight: 700; white-space: nowrap; }
.title { overflow-wrap: anywhere; }
.meta { grid-column: 2; font-size: 0.8rem; color: var(--ink-soft); }
.state { color: var(--ink-soft); }
.all { margin: 0; }
@media (max-width: 34rem) {
  .items a { grid-template-columns: 1fr; }
  .meta { grid-column: 1; }
}
</style>
