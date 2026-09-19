<script setup>
defineProps({
  login: { type: String, required: true },
  levelName: { type: String, required: true },
})
</script>

<template>
  <RouterLink to="/file" class="pass" :aria-label="`Пропуск: ${login}, ${levelName}. Открыть личное дело`">
    <span class="pass-title" aria-hidden="true">Пропуск</span>
    <span class="pass-login">{{ login }}</span>
    <span class="pass-level">{{ levelName }}</span>
  </RouterLink>
</template>

<style scoped>
.pass {
  display: grid;
  gap: 0.05rem;
  min-width: 8rem;
  max-width: 14rem;
  padding: 0.35rem 0.8rem;
  border: 2px solid var(--paper);
  border-radius: var(--radius);
  color: var(--paper);
  text-decoration: none;
  line-height: 1.2;
}
.pass:hover { background: var(--paper); color: var(--desk); }
.pass-title {
  font-family: var(--font-head);
  font-size: 0.7rem;
  letter-spacing: 0.2em;
  text-transform: uppercase;
  opacity: 0.8;
}
.pass-login {
  overflow: hidden;
  font-family: var(--font-head);
  font-size: 1.05rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pass-level { font-size: 0.8rem; }
</style>
