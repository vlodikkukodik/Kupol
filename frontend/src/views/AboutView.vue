<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import AboutContent from '@/components/AboutContent.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { isApiError } from '@/api/client'
import { documentsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { formatComposed } from '@/lib/format'
import { AUTHOR_CONTACT } from '@/content/site'
import ErrorView from './ErrorView.vue'

// «О КУПОЛЕ»: справка, правила, политика конфиденциальности и живая хронология. Страница открыта всем и индексируется.
const timeline = useQuery({ queryKey: keys.timeline, queryFn: ({ signal }) => documentsApi.timeline({ signal }), staleTime: 60_000 })
</script>

<template>
  <div>
    <UiPageHeader title="О КУПОЛЕ" kicker="Справка и правила архива" />
    <UiSheet as="article" data-testid="about">
      <AboutContent :contact="AUTHOR_CONTACT">
        <template #timeline>
          <section id="timeline" aria-labelledby="timeline-title" class="timeline" data-testid="timeline">
            <h2 id="timeline-title">Хронология</h2>
            <p class="timeline__lead">События 1974 года — наших дней. Показаны те, что доступны вашему допуску; часть событий может быть закрыта.</p>
            <UiSkeleton v-if="timeline.isPending.value" :lines="4" label="Загружаем хронологию…" />
            <ErrorView v-else-if="timeline.isError.value" :request-id="isApiError(timeline.error.value) ? timeline.error.value.requestId : ''" :retrying="timeline.isFetching.value" @retry="timeline.refetch()" />
            <p v-else-if="!timeline.data.value?.length" class="timeline__empty">В хронологии пока нет событий, доступных вам.</p>
            <ol v-else class="timeline__list" data-testid="timeline-list">
              <li v-for="e in timeline.data.value" :key="e.id" class="event">
                <p class="event__date">{{ formatComposed(e.date) }}</p>
                <div>
                  <h3 class="event__title">{{ e.title }}</h3>
                  <p v-if="e.body" class="event__body">{{ e.body }}</p>
                  <p v-if="e.level > 0 || e.document" class="event__meta">
                    <template v-if="e.level > 0">допуск {{ e.level }}</template>
                    <template v-if="e.level > 0 && e.document"> · </template>
                    <RouterLink v-if="e.document" :to="{ name: 'document', params: { ref: e.document.slug } }">{{ e.document.code }} — {{ e.document.title }}</RouterLink>
                  </p>
                </div>
              </li>
            </ol>
          </section>
        </template>
      </AboutContent>
    </UiSheet>
  </div>
</template>

<style scoped>
.timeline {
  margin-top: var(--space-6);
  scroll-margin-top: var(--space-4);
}
.timeline h2 {
  margin: 0 0 var(--space-2);
  padding-bottom: var(--space-1);
  border-bottom: 2px solid var(--ink-900);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.timeline__lead,
.timeline__empty {
  color: var(--text-muted);
}
.timeline__list {
  margin: 0;
  padding: 0;
  list-style: none;
  border-left: 3px solid var(--ink-900);
}
.event {
  position: relative;
  display: grid;
  grid-template-columns: 9rem 1fr;
  gap: var(--space-4);
  margin-left: var(--space-4);
  padding: var(--space-2) 0 var(--space-3);
}
.event::before {
  content: '';
  position: absolute;
  top: 0.9rem;
  left: calc(-1 * var(--space-4) - 7px);
  width: 11px;
  height: 11px;
  border: 2px solid var(--ink-900);
  border-radius: 50%;
  background: var(--paper-100);
}
.event__date {
  margin: 0;
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: 0.04em;
}
.event__title {
  margin: 0;
  font-size: var(--text-md);
  overflow-wrap: anywhere;
}
.event__body {
  margin: var(--space-1) 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.event__meta {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
@media (max-width: 40rem) {
  .event {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
