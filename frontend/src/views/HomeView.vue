<script setup>
import { onMounted } from 'vue'
import RecentFeed from '../components/RecentFeed.vue'
import SealMark from '../components/SealMark.vue'
import Stamp from '../components/Stamp.vue'
import { useAuthStore } from '../stores/auth.js'

const auth = useAuthStore()

// Разовое сообщение (например, «дело сдано в архив») забираем при открытии главной.
const flash = auth.takeFlash()

// Гость увидит формы после ответа сервера; при сбое связи формы всё равно доступны (ошибку покажет отправка).
onMounted(() => {
  auth.load().catch(() => {})
})
</script>

<template>
  <article class="door">
    <div class="marking">
      <Stamp text="Форма КУПОЛ-1" tone="ink" :tilt="-2" :animate="false" class="marking-stamp" />
    </div>

    <div class="title">
      <SealMark :size="132" class="title-seal" />
      <div>
        <h1>Купол</h1>
        <p class="full-name">Комитет Управления Паранормальными Объектами и Локациями</p>
      </div>
    </div>

    <p v-if="flash" class="flash" role="status">{{ flash }}</p>

    <p>
      Вы находитесь в Центральном архиве Купола (ЦАК). Доступ к документам определяется
      уровнем допуска.
    </p>

    <section v-if="auth.user" class="welcome" aria-labelledby="welcome-title">
      <h2 id="welcome-title">Допуск оформлен</h2>
      <p>
        Вы вошли как <strong>{{ auth.user.login }}</strong> ({{ auth.user.level_name }}).
      </p>
      <p><RouterLink class="btn" to="/file">Личное дело</RouterLink></p>
    </section>

    <RecentFeed />
  </article>
</template>

<style scoped>
.door { position: relative; }

.marking { display: flex; justify-content: flex-end; margin-bottom: var(--space-4); }
.marking-stamp { font-size: 1rem; }

.title { display: flex; align-items: center; gap: var(--space-4); margin-bottom: var(--space-4); }
.title-seal { flex: none; }
.title h1 { margin-bottom: var(--space-2); }
.full-name {
  margin: 0;
  font-family: var(--font-head);
  font-size: 1.1rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-soft);
}

.flash {
  margin: 0 0 var(--space-4);
  padding: var(--space-3);
  border: 2px solid var(--ink);
  background: var(--paper-shade);
  font-weight: 700;
}

.welcome {
  margin-top: var(--space-5);
  padding: var(--space-4);
  border: 2px solid var(--ink);
  background: var(--paper-shade);
}
.welcome h2 { margin-bottom: var(--space-2); }

@media (max-width: 34rem) {
  .title { flex-direction: column; align-items: flex-start; }
}
</style>
