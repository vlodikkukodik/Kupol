<script setup lang="ts">
import { onMounted, ref } from 'vue'
import UiButton from '@/ui/UiButton.vue'

// Спокойное уведомление о cookie: сайт ставит одну техническую cookie сеанса (после входа), рекламных и аналитических нет — согласия
// оно не требует, но человек вправе знать. Стоит в потоке страницы над подвалом (не всплывает поверх) и не мешает читать;
// «Понятно» запоминается в браузере.
const KEY = 'kupol_cookie_notice'
const shown = ref(false)

onMounted(() => {
  try {
    shown.value = localStorage.getItem(KEY) !== '1'
  } catch {
    shown.value = true // хранилище недоступно (приватный режим): показываем, запомнить не сможем
  }
})

function dismiss() {
  shown.value = false
  try {
    localStorage.setItem(KEY, '1')
  } catch {
    // не запомнилось — при следующем визите покажем снова
  }
}
</script>

<template>
  <aside v-if="shown" class="cookie" aria-labelledby="cookie-title" data-testid="cookie-notice">
    <p id="cookie-title" class="cookie__text">
      {{ $t('cookie.text') }}
      <RouterLink to="/about#privacy">{{ $t('cookie.more') }}</RouterLink>
    </p>
    <UiButton size="sm" data-testid="cookie-dismiss" @click="dismiss">{{ $t('cookie.ok') }}</UiButton>
  </aside>
</template>

<style scoped>
.cookie {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  width: min(100% - 2 * var(--gutter), var(--page-max));
  margin: var(--space-5) auto 0;
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--paper-100);
  color: var(--ink-900);
}
.cookie__text {
  flex: 1 1 18rem;
  margin: 0;
  font-size: var(--text-sm);
}
</style>
