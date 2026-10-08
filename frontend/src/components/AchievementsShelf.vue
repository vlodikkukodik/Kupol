<script setup lang="ts">
// «Полка грамот» в личном деле (шаг 5.6): что уже получено, названия — своим переводом (achievementName),
// не серверным, чтобы не тратить лишний запрос ради названий, которые и так известны фронту.
import { useQuery } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { authApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { achievementName } from '@/lib/achievements'
import { formatDate } from '@/lib/format'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'

const list = useQuery({ queryKey: keys.myAchievements, queryFn: ({ signal }) => authApi.achievements({ signal }), staleTime: 0 })
</script>

<template>
  <UiSkeleton v-if="list.isPending.value" :lines="2" :label="$t('achievements.loading')" />
  <p v-else-if="list.isError.value" role="alert">{{ isApiError(list.error.value) ? list.error.value.message : $t('errors.generic') }}</p>
  <UiEmpty v-else-if="!list.data.value?.length" icon="stamp" :title="$t('achievements.empty')" />
  <ul v-else class="shelf" data-testid="achievements-shelf">
    <li v-for="item in list.data.value" :key="item.kind">
      <span class="shelf__name">{{ achievementName(item.kind) }}</span>
      <span class="shelf__date">{{ $t('achievements.got', { when: formatDate(item.awarded_at) }) }}</span>
    </li>
  </ul>
</template>

<style scoped>
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
.shelf li:first-child {
  border-top: 0;
}
.shelf__name {
  font-weight: 700;
}
.shelf__date {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
