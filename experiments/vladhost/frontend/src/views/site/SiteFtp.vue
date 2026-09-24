<script setup lang="ts">
import { CopyOutline, KeyOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NIcon, NModal, NPopconfirm, NSpace, useMessage } from 'naive-ui'
import { ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { ftpGrantSchema, siteResponseSchema, type Site } from '@/api/schemas'
import { useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const store = useSitesStore()
const message = useMessage()
const busy = ref(false)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

// Выданный FTP-пароль: показывается один раз, на сервере остаётся только хеш.
const grant = ref<{ site: Site; password: string } | null>(null)

async function enable() {
  busy.value = true
  try {
    const r = await api(`/api/sites/${props.site.id}/ftp`, { method: 'POST', schema: ftpGrantSchema })
    grant.value = { site: r.site, password: r.password }
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.ftp.enableFailed')))
  } finally {
    busy.value = false
  }
}

async function disable() {
  busy.value = true
  try {
    await api(`/api/sites/${props.site.id}/ftp`, { method: 'DELETE', schema: siteResponseSchema })
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.ftp.disableFailed')))
  } finally {
    busy.value = false
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('sites.ftp.title') }}</h1>
    </header>

    <n-alert v-if="!site.ftp.available" type="info" :show-icon="false">{{ t('siteArea.ftpUnavailable') }}</n-alert>

    <section v-else class="glass ftp rise" style="--i: 1">
      <span class="ftp-ic"><n-icon :size="18" :component="KeyOutline" /></span>
      <div class="ftp-body">
        <strong>{{ t('sites.ftp.title') }}</strong>
        <span v-if="site.ftp.enabled" class="ftp-line">
          {{ t('sites.ftp.connection', { host: site.ftp.host ?? '', port: site.ftp.port ?? 0, user: site.ftp.username ?? '' }) }}
        </span>
      </div>
      <n-space v-if="site.ftp.enabled" :size="8">
        <n-popconfirm @positive-click="enable">
          <template #trigger>
            <n-button size="small" class="tint-violet" :disabled="busy">{{ t('sites.ftp.newPassword') }}</n-button>
          </template>
          {{ t('sites.ftp.newPasswordConfirm') }}
        </n-popconfirm>
        <n-button size="small" class="tint-rose" :disabled="busy" @click="disable">{{ t('sites.ftp.disable') }}</n-button>
      </n-space>
      <n-button v-else size="small" class="tint-violet" :loading="busy" @click="enable">{{ t('sites.ftp.enable') }}</n-button>
    </section>

    <n-modal :show="grant !== null" preset="card" :title="t('sites.ftp.dialogTitle')" style="max-width: 480px" @update:show="grant = null">
      <template v-if="grant">
        <p class="note">{{ t('sites.ftp.once') }}</p>
        <dl class="creds">
          <dt>{{ t('sites.ftp.server') }}</dt>
          <dd><code>{{ grant.site.ftp.host }}</code></dd>
          <dt>{{ t('sites.ftp.port') }}</dt>
          <dd><code>{{ grant.site.ftp.port }}</code></dd>
          <dt>{{ t('sites.ftp.login') }}</dt>
          <dd>
            <code>{{ grant.site.ftp.username }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.site.ftp.username ?? '')">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('sites.ftp.password') }}</dt>
          <dd>
            <code data-testid="ftp-password" class="pw">{{ grant.password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
        </dl>
        <p class="note">{{ t(grant.site.ftp.allow_plain ? 'sites.ftp.howTo' : 'sites.ftp.howToSecure') }}</p>
      </template>
    </n-modal>
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

.ftp {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  padding: 18px 22px;
}

.ftp-ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-violet);
}

.ftp-body {
  flex: 1;
  min-width: 180px;
  display: grid;
}

.ftp-line,
.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.creds {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 10px 16px;
  margin: 14px 0;
  padding: 14px 16px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.creds dt {
  color: var(--text-faint);
}

.creds dd {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  word-break: break-all;
}

.pw {
  font-size: 15px;
  letter-spacing: 0.04em;
  color: #fde68a;
  background: rgba(251, 191, 36, 0.1);
  border-color: rgba(251, 191, 36, 0.35);
}
</style>
