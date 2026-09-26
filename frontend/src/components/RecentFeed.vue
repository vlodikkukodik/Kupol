<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import UiBadge from '@/ui/UiBadge.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { documentsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { formatComposed } from '@/lib/format'
import { levelName } from '@/lib/levels'

// «Поступило в ЦАК»: последние опубликованные документы, доступные читателю по его допуску.
const LIMIT = 8
const { data, isPending, isError } = useQuery({
  queryKey: keys.recent(LIMIT),
  queryFn: ({ signal }) => documentsApi.recent(LIMIT, { signal }),
})
</script>

<template>
  <section class="feed" aria-labelledby="feed-title">
    <h2 id="feed-title">{{ $t('feed.title') }}</h2>

    <UiSkeleton v-if="isPending" :lines="4" :label="$t('feed.loading')" />
    <p v-else-if="isError" class="state">{{ $t('feed.unavailable') }}</p>
    <template v-else-if="data">
      <ul v-if="data.items.length" class="items" data-testid="recent-feed">
        <li v-for="it in data.items" :key="it.slug">
          <RouterLink :to="{ name: 'document', params: { ref: it.slug } }" class="item">
            <span class="item__code">{{ it.code }}</span>
            <span class="item__title">{{ it.title }}</span>
            <span class="item__meta">{{ it.type_name }}, {{ formatComposed(it.composed) }}</span>
            <UiBadge v-if="it.level > 0" tone="ink" class="item__level" :title="$t('feed.levelTitle', { name: levelName(it.level) })">{{ $t('feed.level', { level: it.level }) }}</UiBadge>
          </RouterLink>
        </li>
      </ul>
      <p v-else class="state" data-testid="recent-empty">{{ $t('feed.empty') }}</p>
      <p class="all"><RouterLink to="/catalog">{{ $t('feed.all') }}</RouterLink></p>
    </template>
  </section>
</template>

<style scoped>
.feed h2 {
  padding-bottom: var(--space-2);
  border-bottom: 2px solid var(--ink-900);
}
.state {
  color: var(--text-muted);
}
.items {
  margin: 0;
  padding: 0;
  list-style: none;
}
.items li {
  border-bottom: 1px dashed var(--border-strong);
}
.item {
  display: grid;
  grid-template-columns: 7.5rem 1fr auto;
  grid-template-areas:
    'code title level'
    'code meta level';
  gap: 0 var(--space-4);
  align-items: baseline;
  padding: var(--space-3) var(--space-2);
  color: var(--text);
  text-decoration: none;
}
.item:hover {
  background: var(--surface-strong);
}
.item__code {
  grid-area: code;
  font-family: var(--font-doc);
  font-size: var(--text-sm);
  font-weight: 700;
  letter-spacing: 0.02em;
}
.item__title {
  grid-area: title;
  font-size: var(--text-lg);
  font-weight: 700;
  text-decoration: underline;
  text-decoration-color: var(--border-strong);
  text-underline-offset: 0.2em;
}
.item:hover .item__title {
  text-decoration-color: currentColor;
}
.item__meta {
  grid-area: meta;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.item__level {
  grid-area: level;
  align-self: center;
}
.all {
  margin: var(--space-3) 0 0;
}
@media (max-width: 36rem) {
  .item {
    grid-template-columns: 1fr auto;
    grid-template-areas:
      'code level'
      'title title'
      'meta meta';
  }
}
</style>
