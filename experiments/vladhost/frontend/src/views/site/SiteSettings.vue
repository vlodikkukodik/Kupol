<script setup lang="ts">
import { TrashOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NPopconfirm, useMessage } from 'naive-ui'
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import type { Site } from '@/api/schemas'
import { useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const store = useSitesStore()
const message = useMessage()
const router = useRouter()
const busy = ref(false)

async function remove() {
  busy.value = true
  try {
    await api(`/api/sites/${props.site.id}`, { method: 'DELETE' })
    await router.push({ name: 'sites' })
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('sites.deleteFailed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.settings') }}</h1>
    </header>

    <section class="glass danger rise" style="--i: 1">
      <h3>{{ t('siteArea.dangerTitle') }}</h3>
      <p>{{ t('siteArea.dangerHint') }}</p>
      <n-popconfirm @positive-click="remove">
        <template #trigger>
          <n-button class="tint-rose" :disabled="busy">
            <template #icon><n-icon :component="TrashOutline" /></template>
            {{ t('sites.deleteSite') }}
          </n-button>
        </template>
        {{ t('sites.deleteConfirm', { host: site.host }) }}
      </n-popconfirm>
    </section>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 900px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.danger {
  padding: 20px 24px;
  border-color: rgba(251, 113, 133, 0.35) !important;
}

.danger p {
  color: var(--text-dim);
  margin: 6px 0 14px;
}
</style>
