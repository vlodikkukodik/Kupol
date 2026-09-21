<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import TemplateCard from '@/components/team/TemplateCard.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from '../ErrorView.vue'

// «Шаблоны»: заготовки документов и наборы блоков. Читают все члены команды; заводятся из документа кнопкой «Сохранить как шаблон»
// (у тех, у кого есть право вести шаблоны).
const route = useRoute()
const router = useRouter()
const client = useQueryClient()

const kind = computed<'document' | 'blockset'>(() => (route.query.kind === 'blockset' ? 'blockset' : 'document'))
const list = useQuery({ queryKey: computed(() => keys.teamTemplates(kind.value)), queryFn: ({ signal }) => teamApi.templates(kind.value, { signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const notice = ref('')

const KINDS = ['document', 'blockset'] as const

function setKind(next: 'document' | 'blockset') {
  void router.replace({ query: next === 'document' ? {} : { kind: next } })
}
async function onChanged(message: string) {
  notice.value = message
  await client.invalidateQueries({ queryKey: ['team', 'templates'] })
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="templates-title" data-testid="templates">
    <h2 id="templates-title">{{ $t('templates.title') }}</h2>
    <p class="lead">{{ $t('templates.lead') }}</p>

    <div class="kinds" role="group" :aria-label="$t('templates.kindsLabel')">
      <button v-for="k in KINDS" :key="k" type="button" class="kind" :aria-pressed="kind === k ? 'true' : 'false'" :data-testid="`kind-${k}`" @click="setKind(k)">{{ $t(`templates.${k}`) }}</button>
    </div>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice" class="notice" data-testid="templates-notice">{{ notice }}</p>

    <UiSkeleton v-if="list.isPending.value" :lines="4" :label="$t('templates.loading')" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="layers" :title="$t(kind === 'document' ? 'templates.emptyDocument' : 'templates.emptyBlockset')">{{ $t(kind === 'document' ? 'templates.hintDocument' : 'templates.hintBlockset') }}</UiEmpty>
    <div v-else class="grid" data-testid="templates-list">
      <TemplateCard v-for="t in list.data.value" :key="t.id" :item="t" @changed="onChanged" />
    </div>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.kinds {
  display: inline-flex;
  margin: var(--space-2) 0 var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  overflow: hidden;
}
.kind {
  min-height: var(--control-h);
  padding: 0 var(--space-5);
  border: 0;
  background: transparent;
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.kind + .kind {
  border-left: 2px solid var(--ink-900);
}
.kind:hover {
  background: var(--surface-strong);
}
.kind[aria-pressed='true'] {
  background: var(--ink-900);
  color: var(--paper-50);
  font-weight: 700;
}
.notice {
  margin: 0 0 var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--success);
  background: var(--surface-sunken);
}
.grid {
  display: grid;
  gap: var(--space-3);
}
@media (max-width: 40rem) {
  .kinds {
    display: flex;
    width: 100%;
  }
  .kind {
    flex: 1 1 auto;
    min-width: 0;
    padding: 0 var(--space-2);
    font-size: var(--text-sm);
  }
}
</style>
