<script setup lang="ts">
import { AddOutline, ChevronForward, GlobeOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, useMessage } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { fieldErrors, siteForm, siteResponseSchema } from '@/api/schemas'
import EmptyState from '@/components/EmptyState.vue'
import StatusChip from '@/components/StatusChip.vue'
import { formatBytes, formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useSitesStore } from '@/stores/sites'

// Здесь только выбор сайта и создание нового: вся работа с сайтом идёт в его отдельном кабинете.
const { t, locale } = useI18n()
const auth = useAuthStore()
const store = useSitesStore()
const message = useMessage()
const router = useRouter()

const form = reactive({ slug: '' })
const errors = ref<Record<string, string>>({})
const creating = ref(false)

const canCreate = computed(() => store.sites.length < store.limits.max_sites)
const usedPercent = computed(() =>
  store.limits.disk_quota_bytes ? Math.min(100, Math.round((store.used / store.limits.disk_quota_bytes) * 100)) : 0,
)
const hostPreview = computed(() => `${form.slug.trim().toLowerCase() || '…'}.${auth.user?.username}.vladinc.ru`)

const load = () => store.load(t('sites.loadFailed'))

async function create() {
  const parsed = siteForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  creating.value = true
  try {
    const r = await api('/api/sites', { method: 'POST', body: parsed.data, schema: siteResponseSchema })
    form.slug = ''
    await load()
    await router.push({ name: 'site-overview', params: { id: r.site.id } })
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(e instanceof ApiError ? e.message : t('sites.createFailed'))
  } finally {
    creating.value = false
  }
}

let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  poll = setInterval(() => {
    if (store.waiting) void load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('sites.title') }}</h1>
      <p>{{ t('sites.pickHint') }}</p>
    </header>

    <n-alert v-if="store.loadError" type="error" :show-icon="false">
      {{ store.loadError }} <n-button size="tiny" @click="load">{{ t('common.retry') }}</n-button>
    </n-alert>

    <section class="disk glass rise" style="--i: 1">
      <div class="disk-top">
        <span class="disk-title">{{ t('sites.disk') }}</span>
        <span class="disk-val">{{ t('sites.diskUsed', { used: formatBytes(store.used, locale), total: formatBytes(store.limits.disk_quota_bytes, locale) }) }}</span>
      </div>
      <div class="meter"><span :style="{ width: `${Math.max(usedPercent, store.used > 0 ? 2 : 0)}%` }" /></div>
    </section>

    <div v-if="store.loading" class="skeletons">
      <div class="skeleton" style="height: 96px" />
    </div>

    <transition-group v-else name="list" tag="div" class="sites">
      <router-link
        v-for="s in store.sites"
        :key="s.id"
        :to="{ name: 'site-overview', params: { id: s.id } }"
        class="site glass lift plain"
      >
        <span class="globe"><n-icon :size="22" :component="GlobeOutline" /></span>
        <div class="titles">
          <span class="host">{{ s.host }}</span>
          <div class="meta">
            <template v-if="s.deployed_at">
              {{ t('sites.updated', { date: formatDateTime(s.deployed_at, locale), size: formatBytes(s.disk_bytes, locale) }) }}
            </template>
            <template v-else>{{ t('sites.uploadHint') }}</template>
          </div>
        </div>
        <div class="chips">
          <status-chip v-if="s.cert_status === 'pending'" tone="amber" pulse>{{ t('sites.cert.pending') }}</status-chip>
          <status-chip v-else-if="s.cert_status === 'failed'" tone="rose">{{ t('sites.cert.failed') }}</status-chip>
          <status-chip v-else-if="s.cert_status === 'active'" tone="cyan">{{ t('sites.cert.active') }}</status-chip>
          <status-chip :tone="s.status === 'live' ? 'emerald' : 'slate'">
            {{ s.status === 'live' ? t('sites.status.live') : t('sites.status.empty') }}
          </status-chip>
        </div>
        <n-icon :size="20" :component="ChevronForward" class="go" />
      </router-link>
    </transition-group>

    <empty-state v-if="!store.loading && !store.sites.length" :title="t('sites.emptyTitle')" :hint="t('sites.emptyHint')" class="glass" />

    <section v-if="canCreate && !store.loading" class="new glass rise">
      <h3>{{ t('sites.newTitle') }}</h3>
      <n-form @submit.prevent="create">
        <n-form-item
          :label="t('sites.name')"
          :validation-status="errors.slug ? 'error' : undefined"
          :feedback="errors.slug ? resolveMessage(errors.slug) : t('sites.addressPreview', { host: hostPreview })"
        >
          <n-input v-model:value="form.slug" size="large" :placeholder="t('sites.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('sites.name') }" />
        </n-form-item>
        <n-button type="primary" size="large" attr-type="submit" :loading="creating">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('common.create') }}
        </n-button>
      </n-form>
    </section>
    <n-alert v-else-if="!store.loading && store.sites.length" type="info" :show-icon="false">
      {{ t('sites.limitReached', { max: store.limits.max_sites }) }}
    </n-alert>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 1080px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.head p {
  margin: 4px 0 0;
  color: var(--text-dim);
}

.disk {
  padding: 18px 22px;
}

.disk-top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  font-weight: 650;
}

.disk-val {
  color: var(--text-dim);
  font-weight: 500;
}

.meter {
  height: 9px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.09);
  overflow: hidden;
}

.meter span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--grad-primary);
  box-shadow: 0 0 16px rgba(139, 92, 246, 0.75);
  transition: width 0.9s var(--ease);
}

.sites {
  position: relative;
  display: grid;
  gap: 14px;
}

.site {
  position: relative;
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  padding: 18px 22px;
  overflow: hidden;
  color: var(--text);
}

/* Цветная полоска слева — фирменный акцент карточки */
.site::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 4px;
  background: var(--grad-primary);
}

.globe {
  display: inline-grid;
  place-items: center;
  width: 46px;
  height: 46px;
  flex: none;
  border-radius: 14px;
  color: #fff;
  background: var(--grad-cyan);
  box-shadow: 0 12px 26px -10px rgba(6, 182, 212, 0.9);
}

.titles {
  flex: 1;
  min-width: 200px;
}

.host {
  font-size: 18px;
  font-weight: 750;
  letter-spacing: -0.015em;
  overflow-wrap: anywhere;
}

.meta {
  margin-top: 2px;
  color: var(--text-dim);
  font-size: 13.5px;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.go {
  color: var(--text-dim);
}

.new {
  padding: 22px 24px;
}

.new h3 {
  margin-bottom: 14px;
  font-size: 17px;
}
</style>
