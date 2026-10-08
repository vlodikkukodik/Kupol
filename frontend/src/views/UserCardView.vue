<script setup lang="ts">
// Карточка пользователя (клик по нику, только вошедшим): ник, уровень, звание и грамоты. История чтения, закладки,
// XP, почта и роли команды не показываются (спецификация §4).
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { useRoute } from 'vue-router'
import { isApiError } from '@/api/client'
import { usersApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { achievementName } from '@/lib/achievements'
import { formatDate } from '@/lib/format'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiStamp from '@/ui/UiStamp.vue'
import ErrorView from './ErrorView.vue'
import NotFoundView from './NotFoundView.vue'

const route = useRoute()
const login = computed(() => String(route.params.login ?? ''))
const card = useQuery({ queryKey: computed(() => keys.userCard(login.value)), queryFn: ({ signal }) => usersApi.card(login.value, { signal }), staleTime: 0, retry: false })
const notFound = computed(() => isApiError(card.error.value) && card.error.value.status === 404)
const requestId = computed(() => (isApiError(card.error.value) ? card.error.value.requestId : ''))
</script>

<template>
  <NotFoundView v-if="notFound" />
  <ErrorView v-else-if="card.isError.value" :request-id="requestId" :retrying="card.isFetching.value" @retry="card.refetch()" />
  <div v-else>
    <UiPageHeader :title="card.data.value?.login ?? login" :kicker="$t('userCard.kicker')" />
    <UiSkeleton v-if="card.isPending.value" :lines="3" :label="$t('userCard.loading')" />
    <UiSheet v-else-if="card.data.value" as="article" data-testid="user-card" :aria-label="$t('userCard.title')">
      <div class="head">
        <p class="level" aria-hidden="true">{{ card.data.value.level }}</p>
        <UiStamp v-if="card.data.value.directorate" :text="card.data.value.level_name" :tilt="-4" />
      </div>
      <dl class="facts">
        <div><dt>{{ $t('file.nick') }}</dt><dd data-testid="card-login">{{ card.data.value.login }}</dd></div>
        <div><dt>{{ $t('file.rank') }}</dt><dd data-testid="card-rank">{{ card.data.value.level_name }}</dd></div>
      </dl>
      <h2>{{ $t('achievements.title') }}</h2>
      <UiEmpty v-if="!card.data.value.achievements.length" icon="stamp" :title="$t('userCard.noAchievements')" />
      <ul v-else class="shelf" data-testid="card-achievements">
        <li v-for="a in card.data.value.achievements" :key="a.kind">
          <span class="shelf__name">{{ achievementName(a.kind) }}</span>
          <span class="shelf__date">{{ $t('achievements.got', { when: formatDate(a.awarded_at) }) }}</span>
        </li>
      </ul>
    </UiSheet>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.level {
  margin: 0;
  display: grid;
  place-items: center;
  width: 4.5rem;
  height: 4.5rem;
  border: 3px solid var(--ink-900);
  border-radius: var(--radius-2);
  font-family: var(--font-head);
  font-size: var(--text-4xl);
  font-weight: 700;
  line-height: 1;
}
.facts {
  margin: 0 0 var(--space-4);
}
.facts > div {
  padding: var(--space-2) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.facts dt {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.facts dd {
  margin: 0;
  font-size: var(--text-lg);
  font-weight: 700;
  overflow-wrap: anywhere;
}
.shelf {
  margin: 0;
  padding: 0;
  list-style: none;
}
.shelf li {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-2) var(--space-3);
  padding: var(--space-2) 0;
  border-top: 1px dashed var(--border-strong);
}
.shelf__name {
  font-weight: 700;
}
.shelf__date {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
