<script setup>
import { computed, nextTick, provide, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/index.js'
import BlockRenderer from '../components/BlockRenderer.vue'
import { useResource } from '../composables/useResource.js'
import { useAuthStore } from '../stores/auth.js'
import AccessDeniedView from './AccessDeniedView.vue'
import ErrorView from './ErrorView.vue'
import NotFoundView from './NotFoundView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const { data, error, loading, reload } = useResource(
  (signal) => api.get(`/documents/${encodeURIComponent(route.params.ref)}`, { signal }),
  // заново — и при смене документа, и когда вошёл или вышел читатель: от его допуска зависит, что закрыто
  () => `${route.params.ref}|${auth.user?.login ?? ''}`,
)
const doc = computed(() => data.value?.document ?? null)
provide('document', doc) // шапка досье берёт свойства документа отсюда

// Заголовок вкладки при отказе: без него остался бы общий «Документ — КУПОЛ», а для закрытого дела — тем более
// нельзя подсказывать больше, чем показывает сама страница.
watch(error, (e) => {
  if (!e) return
  if (e.code === 'access_denied') document.title = 'Доступ запрещён — КУПОЛ'
  else if (e.status === 404) document.title = 'Дело не найдено — КУПОЛ'
})

// Один адрес у документа: О-041, o-41 и т.п. заменяются на канонический латинский (/doc/O-041).
watch(doc, async (d) => {
  if (!d) return
  document.title = `${d.code} — ${d.title} — КУПОЛ`
  if (route.params.ref !== d.slug) {
    await router.replace({ name: 'document', params: { ref: d.slug }, query: route.query, hash: route.hash })
  }
  // после перехода фокус — на название документа (для скринридеров и клавиатуры)
  await nextTick()
  document.querySelector('[data-doc-title]')?.focus()
})
</script>

<template>
  <article v-if="doc" class="dossier" :data-code="doc.code">
    <header class="head">
      <p class="kicker">{{ doc.type_name }} · {{ doc.code }}</p>
      <h1 data-doc-title tabindex="-1">{{ doc.title }}</h1>
    </header>

    <BlockRenderer v-for="(block, i) in doc.blocks" :key="block.id ?? `redacted-${i}`" :block="block" />

    <footer v-if="doc.author" class="foot">Составил(а): <strong>{{ doc.author }}</strong></footer>
  </article>

  <AccessDeniedView v-else-if="error && error.code === 'access_denied'" :level="error.requiredLevel" />
  <NotFoundView v-else-if="error && error.status === 404" />
  <ErrorView v-else-if="error" :request-id="error.requestId" :retrying="loading" @retry="reload" />
  <p v-else class="loading" role="status">Загрузка дела…</p>
</template>

<style scoped>
.head { margin-bottom: var(--space-4); }
.kicker { margin: 0 0 var(--space-1); font-family: var(--font-head); letter-spacing: 0.14em; text-transform: uppercase; color: var(--ink-soft); }
h1 { margin-bottom: 0; overflow-wrap: anywhere; }
h1:focus { outline: none; }
.foot { margin-top: var(--space-6); padding-top: var(--space-3); border-top: 1px solid var(--rule); font-size: 0.9rem; color: var(--ink-soft); }
.loading { color: var(--ink-soft); }
</style>
