<script setup lang="ts">
// Штамп «ДОПУСК ПОВЫШЕН»: показывается один раз сразу после входа, если он поднял уровень по XP (сервер
// сообщает это в LoginResponse.level_up). Живёт вне страницы — вход мог случиться на любом экране.
import { onBeforeUnmount, watch } from 'vue'
import UiStamp from '@/ui/UiStamp.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
let timer: ReturnType<typeof setTimeout> | undefined

watch(
  () => auth.levelUp,
  (on) => {
    clearTimeout(timer)
    if (on) timer = setTimeout(() => auth.dismissLevelUp(), 5000)
  },
)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div v-if="auth.levelUp" class="level-up" role="status" @click="auth.dismissLevelUp()">
    <UiStamp :text="$t('app.levelUp')" tone="red" :tilt="-8" size="lg" />
  </div>
</template>

<style scoped>
.level-up {
  position: fixed;
  inset-block-start: 5rem;
  inset-inline-end: var(--gutter);
  z-index: var(--z-overlay);
  cursor: pointer;
}
</style>
