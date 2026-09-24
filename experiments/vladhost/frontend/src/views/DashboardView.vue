<script setup lang="ts">
import { ChevronForward, GlobeOutline, RocketOutline, ServerOutline, SettingsOutline, ShieldCheckmarkOutline } from '@vicons/ionicons5'
import { NIcon } from 'naive-ui'
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import { sitesSchema, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { formatBytes, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const auth = useAuthStore()

const sites = ref<Site[]>([])
const limits = ref({ max_sites: 1, disk_quota_bytes: 0 })
const loaded = ref(false)

onMounted(async () => {
  try {
    const r = await api('/api/sites', { schema: sitesSchema })
    sites.value = r.sites
    limits.value = r.limits
  } catch {
    // Обзор остаётся рабочим и без цифр: ошибку загрузки подробно покажет страница «Сайты».
  } finally {
    loaded.value = true
  }
})

const used = computed(() => sites.value.reduce((sum, s) => sum + s.disk_bytes, 0))
const percent = computed(() =>
  limits.value.disk_quota_bytes ? Math.min(100, Math.round((used.value / limits.value.disk_quota_bytes) * 100)) : 0,
)

// Общее состояние HTTPS по всем сайтам: проблема важнее ожидания, ожидание важнее «всё хорошо».
const https = computed(() => {
  if (!sites.value.length) return { tone: 'slate', text: 'dashboard.stats.httpsNone', pulse: false } as const
  if (sites.value.some((s) => s.cert_status === 'failed')) return { tone: 'rose', text: 'dashboard.stats.httpsProblem', pulse: false } as const
  if (sites.value.some((s) => s.cert_status === 'pending')) return { tone: 'amber', text: 'dashboard.stats.httpsPending', pulse: true } as const
  return { tone: 'emerald', text: 'dashboard.stats.httpsAllActive', pulse: false } as const
})

const tiles = [
  { to: { name: 'sites' }, icon: RocketOutline, grad: 'var(--grad-primary)', title: 'dashboard.quick.create', hint: 'dashboard.quick.createHint' },
  { to: { name: 'sites' }, icon: GlobeOutline, grad: 'var(--grad-cyan)', title: 'dashboard.quick.manage', hint: 'dashboard.quick.manageHint' },
  { to: { name: 'settings' }, icon: SettingsOutline, grad: 'var(--grad-amber)', title: 'dashboard.quick.account', hint: 'dashboard.quick.accountHint' },
] as const
</script>

<template>
  <div v-if="auth.user" class="page">
    <section class="hero glass rise">
      <div class="hero-text">
        <h1>{{ t('dashboard.greeting', { name: auth.user.username }) }}</h1>
        <p>{{ t('dashboard.lead') }}</p>
      </div>
      <user-avatar :name="auth.user.username" :size="72" class="hero-avatar" />
    </section>

    <section class="stats">
      <article class="stat glass lift rise" style="--i: 1">
        <span class="ic" style="--g: var(--grad-cyan)"><n-icon :size="22" :component="GlobeOutline" /></span>
        <div class="body">
          <div class="label">{{ t('dashboard.stats.sites') }}</div>
          <div class="value">{{ sites.length }}</div>
          <div class="sub">{{ t('dashboard.stats.sitesOf', { max: limits.max_sites }) }}</div>
        </div>
      </article>

      <article class="stat glass lift rise" style="--i: 2">
        <span class="ic" style="--g: var(--grad-violet)"><n-icon :size="22" :component="ServerOutline" /></span>
        <div class="body">
          <div class="label">{{ t('dashboard.stats.disk') }}</div>
          <div class="value">{{ formatBytes(used, locale) }}</div>
          <div class="sub">{{ t('dashboard.stats.diskOf', { total: formatBytes(limits.disk_quota_bytes, locale) }) }}</div>
          <div class="meter"><span :style="{ width: `${Math.max(percent, used > 0 ? 3 : 0)}%` }" /></div>
        </div>
      </article>

      <article class="stat glass lift rise" style="--i: 3">
        <span class="ic" style="--g: var(--grad-emerald)"><n-icon :size="22" :component="ShieldCheckmarkOutline" /></span>
        <div class="body">
          <div class="label">{{ t('dashboard.stats.https') }}</div>
          <div class="chipline">
            <status-chip :tone="https.tone" :pulse="https.pulse">{{ t(https.text) }}</status-chip>
          </div>
        </div>
      </article>
    </section>

    <h2 class="section rise" style="--i: 4">{{ t('dashboard.quick.title') }}</h2>
    <section class="tiles">
      <router-link v-for="(tile, i) in tiles" :key="tile.title" :to="tile.to" class="tile glass lift plain rise" :style="{ '--i': 5 + i, '--g': tile.grad }">
        <span class="ic"><n-icon :size="24" :component="tile.icon" /></span>
        <span class="tx">
          <strong>{{ t(tile.title) }}</strong>
          <small>{{ t(tile.hint) }}</small>
        </span>
        <n-icon class="go" :size="20" :component="ChevronForward" />
      </router-link>
    </section>

    <section class="account glass rise" style="--i: 8">
      <h3>{{ t('dashboard.account.title') }}</h3>
      <dl>
        <dt>{{ t('dashboard.account.email') }}</dt>
        <dd>{{ auth.user.email }}</dd>
        <dt>{{ t('dashboard.account.addresses') }}</dt>
        <dd><code>{site}.{{ auth.user.username }}.vladinc.ru</code></dd>
        <dt>{{ t('dashboard.account.quotas') }}</dt>
        <dd>
          {{
            t('dashboard.account.quotasValue', {
              sites: t('dashboard.account.sitesCount', { max: limits.max_sites }),
              disk: formatBytes(limits.disk_quota_bytes, locale),
            })
          }}
        </dd>
      </dl>
    </section>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 1080px;
}

.hero {
  position: relative;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 30px 34px;
}

.hero::before {
  content: '';
  position: absolute;
  inset: -40% -10% auto auto;
  width: 60%;
  height: 200%;
  background: radial-gradient(closest-side, rgba(236, 72, 153, 0.32), transparent);
  pointer-events: none;
}

.hero::after {
  content: '';
  position: absolute;
  inset: auto auto -60% -8%;
  width: 55%;
  height: 180%;
  background: radial-gradient(closest-side, rgba(99, 102, 241, 0.35), transparent);
  pointer-events: none;
}

.hero-text {
  position: relative;
  z-index: 1;
}

.hero h1 {
  font-size: clamp(26px, 3.4vw, 38px);
  font-weight: 800;
  background: linear-gradient(90deg, #fff 30%, #c4b5fd);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.hero p {
  margin: 6px 0 0;
  color: var(--text-dim);
  font-size: 16px;
}

.hero-avatar {
  position: relative;
  z-index: 1;
}

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 18px;
}

.stat {
  display: flex;
  gap: 16px;
  padding: 22px;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 50px;
  height: 50px;
  flex: none;
  border-radius: 16px;
  color: #fff;
  background: var(--g);
  box-shadow: 0 12px 26px -10px rgba(139, 92, 246, 0.85);
  transition: transform 0.35s var(--ease);
}

.stat:hover .ic,
.tile:hover .ic {
  transform: rotate(-8deg) scale(1.1);
}

.body {
  min-width: 0;
  flex: 1;
}

.label {
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 650;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.value {
  font-size: 30px;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1.15;
}

.sub {
  color: var(--text-dim);
  font-size: 13.5px;
}

.chipline {
  margin-top: 10px;
}

.meter {
  height: 7px;
  margin-top: 10px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.09);
  overflow: hidden;
}

.meter span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--grad-primary);
  box-shadow: 0 0 14px rgba(139, 92, 246, 0.7);
  transition: width 0.9s var(--ease);
}

.section {
  margin-top: 8px;
  font-size: 18px;
  font-weight: 700;
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 18px;
}

.tile {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px;
  color: var(--text);
}

.tile .ic {
  width: 46px;
  height: 46px;
  border-radius: 15px;
}

.tx {
  display: grid;
  min-width: 0;
  flex: 1;
}

.tx strong {
  font-size: 16px;
}

.tx small {
  color: var(--text-dim);
  font-size: 13px;
}

.go {
  color: var(--text-faint);
  transition:
    transform 0.3s var(--ease),
    color 0.3s;
}

.tile:hover .go {
  transform: translateX(5px);
  color: #fff;
}

.account {
  padding: 22px 26px;
}

.account h3 {
  font-size: 16px;
  margin-bottom: 12px;
}

dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 10px 26px;
  margin: 0;
}

dt {
  color: var(--text-faint);
}

dd {
  margin: 0;
  word-break: break-word;
}
</style>
