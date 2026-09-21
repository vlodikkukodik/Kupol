<script setup lang="ts">
// Пропуск в шапке: логин и допуск читателя; ведёт в личное дело.
defineProps<{ login: string; levelName: string; level: number }>()
</script>

<template>
  <RouterLink to="/file" class="pass" :aria-label="$t('app.pass', { login, level: levelName })">
    <span class="pass__level" aria-hidden="true">{{ level }}</span>
    <span class="pass__text">
      <span class="pass__login">{{ login }}</span>
      <span class="pass__name">{{ levelName }}</span>
    </span>
  </RouterLink>
</template>

<style scoped>
.pass {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  max-width: 16rem;
  min-height: var(--control-h);
  padding: 0.25rem var(--space-3) 0.25rem 0.25rem;
  border: 1.5px solid var(--line-on-bg);
  border-radius: var(--radius-2);
  color: var(--on-bg);
  line-height: 1.15;
  text-decoration: none;
}
.pass:hover {
  border-color: var(--on-bg);
  background: rgb(255 255 255 / 0.07);
  color: var(--on-bg);
}
.pass__level {
  flex: none;
  display: grid;
  place-items: center;
  width: 2.1rem;
  height: 2.1rem;
  border-radius: var(--radius-1);
  background: var(--paper-100);
  color: var(--ink-900);
  font-family: var(--font-head);
  font-size: var(--text-lg);
  font-weight: 700;
}
.pass__text {
  display: grid;
  min-width: 0;
}
.pass__login {
  overflow: hidden;
  font-family: var(--font-head);
  font-size: var(--text-md);
  letter-spacing: 0.06em;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pass__name {
  color: var(--on-bg-muted);
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
</style>
