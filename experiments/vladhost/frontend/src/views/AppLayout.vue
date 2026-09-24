<script setup lang="ts">
import { ChevronDown, GlobeOutline, GridOutline, LogOutOutline, SettingsOutline } from '@vicons/ionicons5'
import { NDropdown, NIcon } from 'naive-ui'
import { computed, h, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import BrandLogo from '@/components/BrandLogo.vue'
import LangSwitch from '@/components/LangSwitch.vue'
import StatusChip from '@/components/StatusChip.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { useI18n, type MessageKey } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

interface NavItem {
  name: string
  label: MessageKey
  icon: Component
  grad: string
}

const items: NavItem[] = [
  { name: 'dashboard', label: 'nav.dashboard', icon: GridOutline, grad: 'var(--grad-primary)' },
  { name: 'sites', label: 'nav.sites', icon: GlobeOutline, grad: 'var(--grad-cyan)' },
  { name: 'settings', label: 'nav.settings', icon: SettingsOutline, grad: 'var(--grad-amber)' },
]

// Страница файлов — часть раздела «Сайты».
const active = computed(() => (route.name === 'files' ? 'sites' : String(route.name ?? '')))

const current = computed(() => items.find((i) => i.name === active.value) ?? items[0]!)

const userMenu = computed(() => [
  { label: t('nav.logout'), key: 'logout', icon: () => h(NIcon, null, { default: () => h(LogOutOutline) }) },
])

async function onSelect(key: string) {
  if (key === 'logout') {
    await auth.logout()
    await router.push({ name: 'login' })
  }
}
</script>

<template>
  <div class="shell">
    <aside class="side glass">
      <router-link to="/" class="logo plain"><brand-logo /></router-link>
      <nav class="nav" :aria-label="t('nav.mainMenu')">
        <router-link
          v-for="it in items"
          :key="it.name"
          :to="{ name: it.name }"
          class="item plain"
          :class="{ on: active === it.name }"
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
          <brand-logo class="bar-logo" :size="30" :wordmark="false" />
          <span class="crumb">{{ t(current.label) }}</span>
        </div>
        <div class="bar-right">
          <lang-switch />
          <n-dropdown trigger="click" :options="userMenu" placement="bottom-end" @select="onSelect">
            <button type="button" class="user">
              <user-avatar :name="auth.user?.username ?? '?'" :size="34" />
              <span class="uname">{{ auth.user?.username }}</span>
              <status-chip v-if="auth.isAdmin" tone="violet">{{ t('nav.admin') }}</status-chip>
              <n-icon :size="16" :component="ChevronDown" class="chev" />
            </button>
          </n-dropdown>
        </div>
      </header>

      <main class="content">
        <router-view v-slot="{ Component: view, route: r }">
          <transition name="page" mode="out-in">
            <component :is="view" :key="String(r.name)" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: 250px minmax(0, 1fr);
  gap: 18px;
  min-height: 100vh;
  padding: 18px;
}

.side {
  position: sticky;
  top: 18px;
  align-self: start;
  height: calc(100vh - 36px);
  padding: 22px 14px;
  display: flex;
  flex-direction: column;
  gap: 26px;
}

.logo {
  padding: 0 8px;
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
  padding: 10px 12px;
  border-radius: 14px;
  color: var(--text-dim);
  font-weight: 600;
  transition:
    background 0.25s,
    color 0.25s,
    transform 0.25s var(--ease);
}

.item .ic {
  display: inline-grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 11px;
  color: var(--text-dim);
  background: rgba(255, 255, 255, 0.06);
  transition:
    background 0.3s,
    color 0.3s,
    box-shadow 0.3s,
    transform 0.3s var(--ease);
}

.item:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
  transform: translateX(3px);
}

.item:hover .ic {
  transform: rotate(-8deg) scale(1.08);
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
  padding: 10px 14px 10px 18px;
  border-radius: 20px !important;
}

.bar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.crumb {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--text-dim);
}

.bar-logo {
  display: none;
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
  transition:
    background 0.25s,
    border-color 0.25s,
    transform 0.25s var(--ease);
}

.user:hover {
  background: rgba(167, 139, 250, 0.16);
  border-color: rgba(167, 139, 250, 0.5);
  transform: translateY(-1px);
}

.chev {
  opacity: 0.6;
}

.content {
  min-width: 0;
  padding-bottom: 24px;
}

/* Телефон: боковая панель становится нижней навигацией */
@media (max-width: 860px) {
  .shell {
    grid-template-columns: minmax(0, 1fr);
    padding: 12px 12px 92px;
    gap: 12px;
  }

  .side {
    position: fixed;
    inset: auto 12px 12px 12px;
    top: auto;
    z-index: 30;
    height: auto;
    padding: 8px;
    flex-direction: row;
    justify-content: center;
    border-radius: 22px !important;
  }

  .logo {
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
    padding: 6px 10px;
    font-size: 11.5px;
  }

  .item.on::before {
    display: none;
  }

  .item:hover {
    transform: none;
  }

  .bar {
    top: 12px;
  }

  .bar-logo {
    display: inline-flex;
  }

  .uname,
  .user :deep(.chip),
  .chev {
    display: none;
  }

  .user {
    padding: 3px;
  }

  .crumb {
    display: none;
  }

  .side {
    background: rgba(14, 16, 36, 0.88) !important;
  }
}
</style>
