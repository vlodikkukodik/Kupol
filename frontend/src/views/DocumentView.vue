<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { asDocBlocks } from '@/api/blocks'
import { isApiError } from '@/api/client'
import { documentsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import BlockRenderer from '@/components/document/BlockRenderer.vue'
import { provideDocument } from '@/components/document/context'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiStamp from '@/ui/UiStamp.vue'
import { useAuthStore } from '@/stores/auth'
import AccessDeniedView from './AccessDeniedView.vue'
import ErrorView from './ErrorView.vue'
import NotFoundView from './NotFoundView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const docRef = computed(() => String(route.params.ref))

// Заново — и при смене документа, и когда вошёл или вышел читатель: от его допуска зависит, что закрыто.
const query = useQuery({
  queryKey: computed(() => [...keys.document(docRef.value), auth.user?.login ?? ''] as const),
  queryFn: ({ signal }) => documentsApi.get(docRef.value, { signal }),
  retry: false,
  staleTime: 0,
})
const doc = computed(() => query.data.value?.document ?? null)
const error = computed(() => (isApiError(query.error.value) ? query.error.value : null))
provideDocument(doc) // шапка досье берёт свойства документа отсюда

const blocks = computed(() => asDocBlocks(doc.value?.blocks ?? []))

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
  document.querySelector<HTMLElement>('[data-doc-title]')?.focus()
})
</script>

<template>
  <UiSheet v-if="doc" as="article" class="paper" :data-code="doc.code" fold>
    <header class="paper__head">
      <div class="paper__strip">
        <span>{{ doc.grif }}</span>
        <span>{{ doc.code }}</span>
      </div>
      <p class="paper__kicker">{{ doc.type_name }} · {{ doc.code }}</p>
      <h1 data-doc-title tabindex="-1">{{ doc.title }}</h1>
      <UiStamp v-if="doc.status && doc.status !== 'published'" class="paper__status" :text="doc.status === 'draft' ? 'Черновик' : doc.status === 'review' ? 'На проверке' : 'Архив'" tone="ink" size="sm" />
    </header>

    <div class="paper__body">
      <BlockRenderer v-for="(block, i) in blocks" :key="block.id ?? `redacted-${i}`" :block="block" />
    </div>

    <footer v-if="doc.author" class="paper__foot">Составил(а): <strong>{{ doc.author }}</strong></footer>
  </UiSheet>

  <AccessDeniedView v-else-if="error && error.code === 'access_denied'" :level="error.requiredLevel" />
  <NotFoundView v-else-if="error && error.status === 404" />
  <ErrorView v-else-if="error" :request-id="error.requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiSheet v-else class="paper"><UiSkeleton :lines="8" label="Загрузка дела…" /></UiSheet>
</template>

<style scoped>
.paper {
  margin-top: var(--space-4);
  padding: var(--space-6) var(--space-7);
  font-family: var(--font-doc);
  font-size: var(--text-md);
  line-height: 1.65;
}
.paper__head {
  position: relative;
  margin-bottom: var(--space-5);
}
.paper__strip {
  display: flex;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
  padding-bottom: var(--space-1);
  border-bottom: 3px double var(--ink-900);
  color: var(--text-muted);
  font-size: var(--text-xs);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.paper__kicker {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-family: var(--font-head);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.paper h1 {
  margin-bottom: 0;
  overflow-wrap: anywhere;
}
.paper h1:focus {
  outline: none;
}
.paper__status {
  position: absolute;
  top: var(--space-5);
  right: 0;
}
.paper__foot {
  margin-top: var(--space-7);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
@media (max-width: 48rem) {
  .paper {
    padding: var(--space-5) var(--space-4);
  }
  .paper__status {
    position: static;
    margin-top: var(--space-3);
  }
}
</style>
