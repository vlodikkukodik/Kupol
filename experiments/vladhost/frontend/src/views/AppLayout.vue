<script setup lang="ts">
import { NButton, NLayout, NLayoutContent, NLayoutHeader, NLayoutSider, NMenu, NTag, type MenuOption } from 'naive-ui'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const menu: MenuOption[] = [
  { label: 'Дашборд', key: 'dashboard' },
  { label: 'Сайты', key: 'sites' },
  { label: 'Настройки', key: 'settings' },
]
// Страница файлов — часть раздела «Сайты».
const active = computed(() => (route.name === 'files' ? 'sites' : String(route.name ?? '')))

async function logout() {
  await auth.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <n-layout has-sider class="root">
    <n-layout-sider bordered :width="220" content-style="padding: 8px 0">
      <div class="brand">Vladhost</div>
      <n-menu :value="active" :options="menu" @update:value="(k: string) => router.push({ name: k })" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="header">
        <span class="who">
          {{ auth.user?.username }}
          <n-tag v-if="auth.isAdmin" size="small" type="success" round>admin</n-tag>
        </span>
        <n-button size="small" quaternary @click="logout">Выйти</n-button>
      </n-layout-header>
      <n-layout-content content-style="padding: 24px" class="content">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<style scoped>
.root { height: 100vh; }
.brand { padding: 14px 24px 10px; font-weight: 700; font-size: 18px; letter-spacing: 0.3px; }
.header { height: 52px; display: flex; align-items: center; justify-content: flex-end; gap: 12px; padding: 0 20px; }
.who { display: inline-flex; align-items: center; gap: 8px; }
.content { min-height: calc(100vh - 53px); }
</style>
