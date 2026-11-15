<script setup lang="ts">
import { computed, nextTick, watch, watchEffect } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { documentsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import DocumentPaper from '@/components/document/DocumentPaper.vue'
import DocumentRatings from '@/components/document/DocumentRatings.vue'
import DocumentRemarks from '@/components/document/DocumentRemarks.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import AccessDeniedView from './AccessDeniedView.vue'
import ErrorView from './ErrorView.vue'
import NotFoundView from './NotFoundView.vue'

const route = useRoute()

// Печать на принтер. На iPhone/iPad window.print() в Safari открывает системное окно печати (AirPrint, «Сохранить в PDF»).
// Картинки с loading="lazy" ниже экрана браузер может не успеть загрузить к печати — Safari печатает пустые места.
// Поэтому перед печатью переводим их в eager и ждём загрузки (не дольше пары секунд).
const canPrint = typeof window !== 'undefined' && typeof window.print === 'function'
async function printDoc() {
  const imgs = Array.from(document.querySelectorAll<HTMLImageElement>('img[data-print-eager]'))
  imgs.forEach((img) => (img.loading = 'eager'))
  const pending = imgs.filter((img) => !img.complete).map((img) => new Promise<void>((done) => {
    img.addEventListener('load', () => done(), { once: true })
    img.addEventListener('error', () => done(), { once: true })
  }))
  await Promise.race([Promise.all(pending), new Promise((done) => setTimeout(done, 2500))])
  window.print()
}
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

// Заголовок вкладки при отказе: без него остался бы общий «Документ — КУПОЛ», а для закрытого дела — тем более
// нельзя подсказывать больше, чем показывает сама страница.
const tabTitle = computed(() => {
  if (doc.value) return t('title.withBase', { name: `${doc.value.code} — ${doc.value.title}` })
  const e = error.value
  if (e?.code === 'access_denied') return t('title.withBase', { name: t('notice.denied.title') })
  if (e?.status === 404) return t('title.withBase', { name: t('title.notFound') })
  return ''
})
// пересчитывается и при смене языка интерфейса (роутер этот маршрут не трогает, см. router/index.ts)
watchEffect(() => {
  if (tabTitle.value) document.title = tabTitle.value
})

// Один адрес у документа: О-041, o-41 и т.п. заменяются на канонический латинский (/doc/O-041).
watch(doc, async (d) => {
  if (!d) return
  if (route.params.ref !== d.slug) {
    await router.replace({ name: 'document', params: { ref: d.slug }, query: route.query, hash: route.hash })
  }
  await nextTick()
  // Ссылка из поиска ведёт к блоку (#b-<id>): прокручиваем к нему, отмечаем и ставим фокус. Блока нет (закрыт этому читателю
  // или удалён) — как обычно, фокус на название.
  const id = route.hash.startsWith('#b-') ? decodeURIComponent(route.hash.slice(1)) : ''
  const target = id ? document.getElementById(id) : null
  if (target) {
    target.setAttribute('tabindex', '-1')
    target.setAttribute('data-found', '')
    target.focus({ preventScroll: true })
    target.scrollIntoView({ block: 'center' })
    return
  }
  // после перехода фокус — на название документа (для скринридеров и клавиатуры)
  document.querySelector<HTMLElement>('[data-doc-title]')?.focus()
})
</script>

<template>
  <template v-if="doc">
    <div v-if="canPrint" class="doc-actions no-print">
      <UiButton icon="printer" size="sm" variant="ghost" data-testid="print-doc" @click="printDoc">{{ $t('doc.print') }}</UiButton>
    </div>
    <DocumentPaper :doc="doc" />
    <DocumentRatings class="no-print" :code="doc.code" />
    <DocumentRemarks class="no-print" :code="doc.code" />
  </template>

  <AccessDeniedView v-else-if="error && error.code === 'access_denied'" :level="error.requiredLevel" />
  <NotFoundView v-else-if="error && error.status === 404" />
  <ErrorView v-else-if="error" :request-id="error.requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />
  <UiSheet v-else class="paper"><UiSkeleton :lines="8" :label="$t('doc.loading')" /></UiSheet>
</template>

<style scoped>
.doc-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-4);
}
.doc-actions + :deep(.paper) {
  margin-top: var(--space-2);
}
/* Лист-заготовка, пока документ грузится: те же поля, что у настоящего листа (DocumentPaper) */
.paper {
  margin-top: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
@media (max-width: 48rem) {
  .paper {
    padding: var(--space-5) var(--space-4);
  }
}
</style>
