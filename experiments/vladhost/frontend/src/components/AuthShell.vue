<script setup lang="ts">
import { CodeSlashOutline, KeyOutline, RocketOutline } from '@vicons/ionicons5'
import { NIcon } from 'naive-ui'
import { useI18n } from '@/i18n'
import BrandLogo from './BrandLogo.vue'
import LangSwitch from './LangSwitch.vue'

// Общая оболочка страниц входа и регистрации: слева — что даёт сервис, справа — форма (слот).
const { t } = useI18n()
</script>

<template>
  <div class="auth">
    <header class="top">
      <brand-logo />
      <lang-switch />
    </header>

    <main class="body">
      <section class="hero">
        <h1 class="headline rise" style="--i: 0">
          <span class="gradient-text">{{ t('auth.hero.headline') }}</span>
        </h1>
        <p class="lead rise" style="--i: 1">{{ t('auth.hero.lead') }}</p>
        <ul class="points">
          <li class="rise" style="--i: 2">
            <span class="ic" style="--g: var(--grad-primary)"><n-icon :size="20" :component="RocketOutline" /></span>
            {{ t('auth.hero.point1') }}
          </li>
          <li class="rise" style="--i: 3">
            <span class="ic" style="--g: var(--grad-cyan)"><n-icon :size="20" :component="CodeSlashOutline" /></span>
            {{ t('auth.hero.point2') }}
          </li>
          <li class="rise" style="--i: 4">
            <span class="ic" style="--g: var(--grad-emerald)"><n-icon :size="20" :component="KeyOutline" /></span>
            {{ t('auth.hero.point3') }}
          </li>
        </ul>
      </section>

      <section class="card glass rise" style="--i: 2">
        <slot />
      </section>
    </main>
  </div>
</template>

<style scoped>
.auth {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  padding: 0 clamp(16px, 4vw, 56px);
}

.top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 22px 0;
}

.body {
  flex: 1;
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(360px, 460px);
  align-items: center;
  gap: clamp(28px, 6vw, 96px);
  max-width: 1200px;
  width: 100%;
  margin: 0 auto;
  padding-bottom: 40px;
}

.headline {
  font-size: clamp(34px, 5vw, 58px);
  line-height: 1.05;
  font-weight: 800;
  letter-spacing: -0.04em;
}

.lead {
  margin: 18px 0 30px;
  max-width: 46ch;
  font-size: 18px;
  color: var(--text-dim);
}

.points {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 14px;
}

.points li {
  display: flex;
  align-items: center;
  gap: 14px;
  color: var(--text);
  font-weight: 550;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: 13px;
  color: #fff;
  background: var(--g);
  box-shadow: 0 10px 24px -10px rgba(139, 92, 246, 0.8);
  transition: transform 0.3s var(--ease);
}

.points li:hover .ic {
  transform: translateY(-3px) rotate(-6deg);
}

.card {
  padding: clamp(22px, 3vw, 34px);
}

@media (max-width: 900px) {
  .body {
    grid-template-columns: minmax(0, 1fr);
    gap: 26px;
  }

  .points,
  .lead {
    display: none;
  }

  .headline {
    font-size: 30px;
  }
}
</style>
