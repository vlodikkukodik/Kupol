<script setup lang="ts">
import { onMounted } from 'vue'
import RecentFeed from '@/components/RecentFeed.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSeal from '@/ui/UiSeal.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiStamp from '@/ui/UiStamp.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()

// Разовое сообщение (например, «дело сдано в архив») забираем при открытии главной.
const flash = auth.takeFlash()

// Гость увидит приглашение войти после ответа сервера; при сбое связи ошибку покажет сама отправка формы.
onMounted(() => {
  auth.load().catch(() => {})
})
</script>

<template>
  <UiSheet as="article" fold class="door">
    <div class="door__marking">
      <UiStamp text="Форма КУПОЛ-1" tone="ink" :tilt="-2" :animate="false" size="sm" />
    </div>

    <div class="door__title">
      <UiSeal :size="132" class="door__seal" />
      <div>
        <h1>Купол</h1>
        <p class="door__full">Комитет Управления Паранормальными Объектами и Локациями</p>
      </div>
    </div>

    <UiAlert v-if="flash" tone="success">{{ flash }}</UiAlert>

    <p class="door__lead">
      Вы находитесь в Центральном архиве Купола (ЦАК). Доступ к документам определяется уровнем допуска: чем выше уровень,
      тем меньше в тексте закрытого.
    </p>

    <section v-if="auth.user" class="door__welcome" aria-labelledby="welcome-title">
      <h2 id="welcome-title">Допуск оформлен</h2>
      <p>
        Вы вошли как <strong>{{ auth.user.login }}</strong> ({{ auth.user.level_name }}).
      </p>
      <UiButton to="/file" variant="primary" icon="user">Личное дело</UiButton>
    </section>
    <section v-else-if="auth.status === 'ready'" class="door__welcome" aria-labelledby="guest-title">
      <h2 id="guest-title">Уровень 0 · Гражданин</h2>
      <p>Вам открыты общедоступные дела. Оформите допуск, чтобы читать больше, — почта не нужна.</p>
      <UiButton variant="primary" icon="user" @click="ui.openAuth()">Получить допуск</UiButton>
    </section>

    <RecentFeed />
  </UiSheet>
</template>

<style scoped>
.door {
  margin-top: var(--space-4);
}
.door__marking {
  /* гриф — в правом верхнем углу: на это место указывает вопрос анкеты при регистрации (accounts/captcha.go) */
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--space-5);
}
.door__title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-5);
  margin-bottom: var(--space-5);
}
.door__title > div {
  flex: 1 1 18rem;
  min-width: 0;
}
.door__title h1 {
  margin: 0;
  font-size: clamp(3rem, 12vw, 5.5rem);
  letter-spacing: 0.22em;
}
.door__full {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-lg);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.door__lead {
  max-width: 40rem;
  font-size: var(--text-lg);
}
.door__welcome {
  margin: var(--space-5) 0;
  padding: var(--space-4) var(--space-5);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
}
.door__welcome h2 {
  font-size: var(--text-lg);
}
.door__welcome p {
  margin-bottom: var(--space-3);
}
</style>
