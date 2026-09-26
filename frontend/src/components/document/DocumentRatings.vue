<script setup lang="ts">
// «Оценки» (шаг 5.3, спецификация §8): «ознакомлен / одобряю / сомнительно» — канцелярские
// отметки читателей под документом. Публичные счётчики штампами с числами (видит каждый
// залогиненный); +5 XP за оценку (до 10/день).
import { computed } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import type { DocumentRatings, Rating } from '@/api/generated/documents'
import { ratingsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiButton from '@/ui/UiButton.vue'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ code: string }>()
const auth = useAuthStore()
const ui = useUiStore()
const client = useQueryClient()

const query = useQuery({
  queryKey: computed(() => keys.ratings(props.code)),
  queryFn: ({ signal }) => ratingsApi.get(props.code, { signal }),
  staleTime: 0,
})

const data = computed<DocumentRatings | null>(() => query.data.value ?? null)
const myRating = computed(() => data.value?.my?.rating ?? null)

const items = computed(() =>
  (['acknowledged', 'approved', 'doubtful'] as const).map((r) => ({
    rating: r,
    count: data.value?.counts?.[r === 'acknowledged' ? 'acknowledged' : r === 'approved' ? 'approved' : 'doubtful'] ?? 0,
    active: myRating.value === r,
    label: t(`doc.ratings.${r}`),
  })),
)

const loading = computed(() => query.isPending.value)

async function toggle(rating: Rating) {
  if (!auth.isAuthenticated) {
    ui.openAuth()
    return
  }
  await ratingsApi.set(props.code, rating)
  await client.invalidateQueries({ queryKey: keys.ratings(props.code) })
}
</script>

<template>
  <UiSheet as="section" class="ratings" aria-labelledby="ratings-title">
    <h2 id="ratings-title">{{ $t('doc.ratings.title') }}</h2>

    <UiSkeleton v-if="loading" :lines="1" :label="$t('doc.ratings.title')" />
    <div v-else class="ratings__stamps">
      <button
        v-for="item in items"
        :key="item.rating"
        type="button"
        class="ratings__stamp"
        :class="{ 'ratings__stamp--active': item.active }"
        :aria-pressed="item.active"
        :title="item.label"
        @click="toggle(item.rating)"
      >
        <span class="ratings__stamp-label">{{ item.label }}</span>
        <span v-if="item.count > 0" class="ratings__stamp-count">{{ item.count }}</span>
      </button>
    </div>

    <p v-if="!auth.isAuthenticated && !loading" class="ratings__login">
      {{ $t('doc.ratings.needLogin') }}
      <UiButton variant="link" @click="ui.openAuth()">{{ $t('doc.ratings.signIn') }}</UiButton>
    </p>
  </UiSheet>
</template>

<style scoped>
.ratings {
  margin-top: var(--space-4);
}
.ratings__stamps {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.ratings__stamp {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0.12em 0.55em;
  border: 0.14em solid var(--ink-900);
  border-radius: 0.2em;
  background: none;
  color: var(--ink-900);
  font-family: var(--font-head);
  font-weight: 700;
  font-size: var(--text-md);
  letter-spacing: 0.14em;
  line-height: 1.2;
  text-transform: uppercase;
  transform: rotate(-6deg);
  mix-blend-mode: multiply;
  opacity: 0.9;
  cursor: pointer;
  transition: opacity 0.15s, transform 0.15s;
  -webkit-mask: var(--texture-ink);
  mask: var(--texture-ink);
  -webkit-mask-size: 260px 260px;
  mask-size: 260px 260px;
}
.ratings__stamp:hover {
  opacity: 1;
  transform: rotate(-3deg) scale(1.05);
}
.ratings__stamp--active {
  color: var(--red-700);
  border-color: var(--red-700);
  opacity: 1;
}
.ratings__stamp-label {
  pointer-events: none;
}
.ratings__stamp-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 1.4em;
  height: 1.4em;
  padding: 0 0.3em;
  border-radius: 50%;
  background: currentColor;
  color: var(--paper);
  font-size: 0.7em;
  line-height: 1;
  pointer-events: none;
}
.ratings__stamp--active .ratings__stamp-count {
  color: var(--red-700);
  background: var(--red-700);
  color: var(--paper);
}
.ratings__login {
  margin-top: var(--space-2);
  color: var(--text-muted);
}
</style>
