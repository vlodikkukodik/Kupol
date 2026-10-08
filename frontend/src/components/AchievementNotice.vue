<script setup lang="ts">
// Уведомление о новых грамотах (шаг 5.6): показывается сразу после входа/регистрации или погашения
// скрытого кода, если сервер сообщил о них (auth.newAchievements). Живёт вне страницы — по образцу
// LevelUpNotice.vue.
import { onBeforeUnmount, watch } from 'vue'
import { achievementName } from '@/lib/achievements'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
let timer: ReturnType<typeof setTimeout> | undefined

watch(
  () => auth.newAchievements.length,
  (n) => {
    clearTimeout(timer)
    if (n > 0) timer = setTimeout(() => auth.dismissNewAchievements(), 6000)
  },
)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div v-if="auth.newAchievements.length" class="achievements" role="status" @click="auth.dismissNewAchievements()">
    <p class="achievements__title">{{ $t('achievements.newTitle') }}</p>
    <ul>
      <li v-for="kind in auth.newAchievements" :key="kind">{{ achievementName(kind) }}</li>
    </ul>
  </div>
</template>

<style scoped>
.achievements {
  position: fixed;
  inset-block-start: 5rem;
  inset-inline-end: var(--gutter);
  z-index: var(--z-overlay);
  max-width: 20rem;
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--ink-900);
  background: var(--paper-50);
  box-shadow: 3px 3px 0 var(--ink-900);
  cursor: pointer;
}
.achievements__title {
  margin: 0 0 var(--space-1);
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.achievements ul {
  margin: 0;
  padding-left: 1.2em;
}
</style>
