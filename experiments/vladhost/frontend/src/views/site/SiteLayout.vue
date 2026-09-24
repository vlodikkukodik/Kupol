<script setup lang="ts">
import {
  ArrowBack,
  FolderOpenOutline,
  GlobeOutline,
  KeyOutline,
  LinkOutline,
  LogOutOutline,
  SettingsOutline,
  SpeedometerOutline,
} from '@vicons/ionicons5'
import { NDropdown, NIcon } from 'naive-ui'
import { computed, h, onBeforeUnmount, onMounted, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LangSwitch from '@/components/LangSwitch.vue'
import StatusChip from '@/components/StatusChip.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { useI18n, type MessageKey } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useSitesStore } from '@/stores/sites'

// Отдельный «кабинет» выбранного сайта: свой каркас, своё меню, никакого общего меню панели.
const { t } = useI18n()
const auth = useAuthStore()
const store = useSitesStore()
const route = useRoute()
const router = useRouter()

interface NavItem {
  name: string
  label: MessageKey
  icon: Component
  grad: string
}

const items: NavItem[] = [
  { name: 'site-overview', label: 'siteArea.overview', icon: SpeedometerOutline, grad: 'var(--grad-primary)' },
  { name: 'files', label: 'siteArea.files', icon: FolderOpenOutline, grad: 'var(--grad-cyan)' },
  { name: 'site-domains', label: 'siteArea.domains', icon: LinkOutline, grad: 'var(--grad-emerald)' },
  { name: 'site-ftp', label: 'siteArea.ftp', icon: KeyOutline, grad: 'var(--grad-violet)' },
  { name: 'site-settings', label: 'siteArea.settings', icon: SettingsOutline, grad: 'var(--grad-amber)' },
]

const siteId = computed(() => Number(route.params.id))
const site = computed(() => store.byId(siteId.value))
const current = computed(() => items.find((i) => i.name === route.name) ?? items[0]!)

const userMenu = computed(() => [
  { label: t('nav.logout'), key: 'logout', icon: () => h(NIcon, null, { default: () => h(LogOutOutline) }) },
])

async function onSelect(key: string) {
  if (key === 'logout') {
    await auth.logout()
    await router.push({ name: 'login' })
  }
}

// Пока выпускается сертификат, обновляем данные сами.
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void store.load(t('sites.loadFailed'))
  poll = setInterval(() => {
    if (store.waiting) void store.load(t('sites.loadFailed'))
  }, 5000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="shell">
    <aside class="side glass">
      <router-link :to="{ name: 'sites' }" class="back plain">
        <span class="ic"><n-icon :size="18" :component="ArrowBack" /></span>
        <span>{{ t('siteArea.allSites') }}</span>
      </router-link>

      <div class="who">
        <span class="globe"><n-icon :size="22" :component="GlobeOutline" /></span>
        <div class="who-text">
          <strong class="host">{{ site?.host ?? '…' }}</strong>
          <status-chip v-if="site" :tone="site.status === 'live' ? 'emerald' : 'slate'">
            {{ site.status === 'live' ? t('sites.status.live') : t('sites.status.empty') }}
          </status-chip>
        </div>
      </div>

      <nav class="nav" :aria-label="t('siteArea.menu')">
        <router-link
          v-for="it in items"
          :key="it.name"
          :to="{ name: it.name, params: { id: siteId } }"
          class="item plain"
          :class="{ on: route.name === it.name }"
          :style="{ '--g': it.grad }"
        >
          <span class="ic"><n-icon :size="19" :component="it.icon" /></span>
          <span class="label">{{ t(it.label) }}</span>
        </router-link>
      </nav>
    </aside>

    <div class="main">
      <header class="bar glass">
        <div class="bar-left">
          <router-link :to="{ name: 'sites' }" class="crumb-link plain">{{ t('nav.sites') }}</router-link>
          <span class="sep">/</span>
          <span class="crumb-site">{{ site?.host ?? '…' }}</span>
          <span class="sep">/</span>
          <span class="crumb">{{ t(current.label) }}</span>
        </div>
        <div class="bar-right">
          <lang-switch />
          <n-dropdown trigger="click" :options="userMenu" placement="bottom-end" @select="onSelect">
            <button type="button" class="user">
              <user-avatar :name="auth.user?.username ?? '?'" :size="34" />
              <span class="uname">{{ auth.user?.username }}</span>
            </button>
          </n-dropdown>
        </div>
      </header>

      <main class="content">
        <div v-if="!store.loading && !site" class="glass missing">{{ t('siteArea.notFound') }}</div>
        <router-view v-else-if="site" v-slot="{ Component: view, route: r }">
          <transition name="page" mode="out-in">
            <component :is="view" :key="String(r.name)" :site="site" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 18px;
  min-height: 100vh;
  padding: 18px;
}

.side {
  position: sticky;
  top: 18px;
  align-self: start;
  height: calc(100vh - 36px);
  padding: 18px 14px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.back {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border-radius: 12px;
  color: var(--text-dim);
  font-weight: 600;
  transition:
    background 0.25s,
    color 0.25s;
}

.back:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
}

.back .ic {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.07);
}

.who {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.globe {
  display: inline-grid;
  place-items: center;
  width: 42px;
  height: 42px;
  flex: none;
  border-radius: 13px;
  color: #fff;
  background: var(--grad-cyan);
  box-shadow: 0 10px 22px -10px rgba(6, 182, 212, 0.9);
}

.who-text {
  display: grid;
  gap: 5px;
  min-width: 0;
  justify-items: start;
}

.host {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.nav {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 13px;
  padding: 9px 12px;
  border-radius: 14px;
  color: var(--text-dim);
  font-weight: 600;
  transition:
    background 0.25s,
    color 0.25s;
}

.item .ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: rgba(255, 255, 255, 0.06);
  transition:
    background 0.3s,
    box-shadow 0.3s;
}

.item:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
}

.item.on {
  color: #fff;
  background: rgba(255, 255, 255, 0.09);
}

.item.on .ic {
  color: #fff;
  background: var(--g);
  box-shadow: 0 8px 22px -6px rgba(139, 92, 246, 0.8);
}

.item.on::before {
  content: '';
  position: absolute;
  left: -14px;
  top: 12px;
  bottom: 12px;
  width: 4px;
  border-radius: 0 4px 4px 0;
  background: var(--g);
}

.main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.bar {
  position: sticky;
  top: 18px;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px 10px 18px;
  border-radius: 20px !important;
}

.bar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-weight: 650;
}

.crumb-link,
.sep,
.crumb-site {
  color: var(--text-dim);
}

.crumb-site {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bar-right {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-left: auto;
}

.user {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 4px 12px 4px 4px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.05);
  color: var(--text);
  font: inherit;
  font-weight: 650;
  cursor: pointer;
}

.content {
  min-width: 0;
  padding-bottom: 24px;
}

.missing {
  padding: 24px;
}

/* Телефон: меню сайта — нижняя панель */
@media (max-width: 860px) {
  .shell {
    grid-template-columns: minmax(0, 1fr);
    padding: 12px 12px 92px;
    gap: 12px;
  }

  .side {
    position: fixed;
    inset: auto 12px 12px 12px;
    z-index: 30;
    height: auto;
    padding: 8px;
    border-radius: 22px !important;
    background: rgba(14, 16, 36, 0.88) !important;
  }

  .back span:not(.ic),
  .who {
    display: none;
  }

  .back {
    display: none;
  }

  .nav {
    flex-direction: row;
    width: 100%;
    justify-content: space-around;
  }

  .item {
    flex-direction: column;
    gap: 3px;
    padding: 6px 8px;
    font-size: 11px;
  }

  .item.on::before {
    display: none;
  }

  .bar {
    top: 12px;
  }

  .crumb-site,
  .uname,
  .sep:first-of-type {
    display: none;
  }
}
</style>
