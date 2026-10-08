<script setup lang="ts">
import { NAlert, NButton } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { activityListSchema, type ActivityEvent } from '@/api/schemas'
import { formatDateTime, useI18n } from '@/i18n'

const { t, locale } = useI18n()

const events = ref<ActivityEvent[]>([])
const next = ref(0)
const category = ref('')
const loading = ref(false)
const loadError = ref('')

async function load(more: boolean) {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (category.value) params.set('category', category.value)
    if (more && next.value) params.set('before', String(next.value))
    const r = await api(`/api/activity?${params}`, { schema: activityListSchema })
    events.value = more ? [...events.value, ...r.events] : r.events
    next.value = r.next
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : t('activity.loadFailed')
  } finally {
    loading.value = false
  }
}
onMounted(() => load(false))

function pick(c: string) {
  category.value = c
  void load(false)
}

// Названия событий и видов перечислены явно: сторож i18n ищет ключи в коде как строки.
const KINDS: Record<string, () => string> = {
  'auth.login': () => t('activity.kinds.auth_login'),
  'auth.login_failed': () => t('activity.kinds.auth_login_failed'),
  'auth.logout': () => t('activity.kinds.auth_logout'),
  'auth.register': () => t('activity.kinds.auth_register'),
  'auth.password_change': () => t('activity.kinds.auth_password_change'),
  'auth.password_reset': () => t('activity.kinds.auth_password_reset'),
  'auth.email_verified': () => t('activity.kinds.auth_email_verified'),
  'auth.email_verify_sent': () => t('activity.kinds.auth_email_verify_sent'),
  'profile.update': () => t('activity.kinds.profile_update'),
  'admin.invite': () => t('activity.kinds.admin_invite'),
  'site.create': () => t('activity.kinds.site_create'),
  'site.delete': () => t('activity.kinds.site_delete'),
  'site.deploy': () => t('activity.kinds.site_deploy'),
  'site.settings': () => t('activity.kinds.site_settings'),
  'cert.renew': () => t('activity.kinds.cert_renew'),
  'domain.add': () => t('activity.kinds.domain_add'),
  'domain.update': () => t('activity.kinds.domain_update'),
  'domain.remove': () => t('activity.kinds.domain_remove'),
  'domain.sub_add': () => t('activity.kinds.domain_sub_add'),
  'ftp.enable': () => t('activity.kinds.ftp_enable'),
  'ftp.disable': () => t('activity.kinds.ftp_disable'),
  'ftp.login': () => t('activity.kinds.ftp_login'),
  'ftp.account_add': () => t('activity.kinds.ftp_account_add'),
  'ftp.account_update': () => t('activity.kinds.ftp_account_update'),
  'ftp.password': () => t('activity.kinds.ftp_password'),
  'ftp.account_delete': () => t('activity.kinds.ftp_account_delete'),
  'files.change': () => t('activity.kinds.files_change'),
  'backup.create': () => t('activity.kinds.backup_create'),
  'backup.restore': () => t('activity.kinds.backup_restore'),
  'backup.download': () => t('activity.kinds.backup_download'),
  'backup.archive': () => t('activity.kinds.backup_archive'),
  'shell.change': () => t('activity.kinds.shell_change'),
  'shell.terminal': () => t('activity.kinds.shell_terminal'),
  'ssh.key_add': () => t('activity.kinds.ssh_key_add'),
  'ssh.key_generate': () => t('activity.kinds.ssh_key_generate'),
  'ssh.key_delete': () => t('activity.kinds.ssh_key_delete'),
  'cms.install': () => t('activity.kinds.cms_install'),
  'runtime.set': () => t('activity.kinds.runtime_set'),
  'runtime.restart': () => t('activity.kinds.runtime_restart'),
  'cron.create': () => t('activity.kinds.cron_create'),
  'cron.update': () => t('activity.kinds.cron_update'),
  'cron.delete': () => t('activity.kinds.cron_delete'),
  'cron.run': () => t('activity.kinds.cron_run'),
  'db.create': () => t('activity.kinds.db_create'),
  'db.delete': () => t('activity.kinds.db_delete'),
  'db.password': () => t('activity.kinds.db_password'),
  'db.addrs': () => t('activity.kinds.db_addrs'),
  'db.web': () => t('activity.kinds.db_web'),
  'mail.domain_add': () => t('activity.kinds.mail_domain_add'),
  'mail.domain_update': () => t('activity.kinds.mail_domain_update'),
  'mail.domain_delete': () => t('activity.kinds.mail_domain_delete'),
  'mail.dns_auto': () => t('activity.kinds.mail_dns_auto'),
  'mail.mailbox_add': () => t('activity.kinds.mail_mailbox_add'),
  'mail.mailbox_update': () => t('activity.kinds.mail_mailbox_update'),
  'mail.mailbox_rules': () => t('activity.kinds.mail_mailbox_rules'),
  'mail.mailbox_password': () => t('activity.kinds.mail_mailbox_password'),
  'mail.mailbox_delete': () => t('activity.kinds.mail_mailbox_delete'),
  'mail.alias_set': () => t('activity.kinds.mail_alias_set'),
  'mail.alias_delete': () => t('activity.kinds.mail_alias_delete'),
  'dns.zone_add': () => t('activity.kinds.dns_zone_add'),
  'dns.zone_delete': () => t('activity.kinds.dns_zone_delete'),
  'dns.record_add': () => t('activity.kinds.dns_record_add'),
  'dns.record_update': () => t('activity.kinds.dns_record_update'),
  'dns.record_delete': () => t('activity.kinds.dns_record_delete'),
}
const kindLabel = (k: string) => KINDS[k]?.() ?? t('activity.other')
const categoryLabel = (c: string) =>
  c === 'security' ? t('activity.categories.security') : c === 'sites' ? t('activity.categories.sites') : c === 'access' ? t('activity.categories.access') : t('activity.categories.services')

const CATEGORIES = ['security', 'sites', 'access', 'services']

// Программа клиента — коротко (браузер или утилита), полная строка остаётся во всплывающей подсказке.
function agentLabel(ua: string): string {
  if (!ua) return ''
  const rules: [RegExp, string][] = [
    [/Edg\//, 'Edge'],
    [/OPR\/|Opera/, 'Opera'],
    [/Firefox\//, 'Firefox'],
    [/Chrome\//, 'Chrome'],
    [/Safari\//, 'Safari'],
    [/curl\//i, 'curl'],
  ]
  return rules.find(([re]) => re.test(ua))?.[1] ?? ua.slice(0, 24)
}

const when = (iso: string) => formatDateTime(iso, locale.value)
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('activity.title') }}</h1>
      <p class="note">{{ t('activity.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <div class="chips">
      <n-button size="small" :type="category === '' ? 'primary' : 'default'" data-testid="activity-cat-all" @click="pick('')">{{ t('activity.all') }}</n-button>
      <n-button v-for="c in CATEGORIES" :key="c" size="small" :type="category === c ? 'primary' : 'default'" :data-testid="`activity-cat-${c}`" @click="pick(c)">{{ categoryLabel(c) }}</n-button>
    </div>

    <section class="glass card rise">
      <p v-if="!events.length && !loading && !loadError" class="note" data-testid="activity-empty">{{ t('activity.empty') }}</p>
      <ul v-else class="events" data-testid="activity-list">
        <li v-for="e in events" :key="e.id" class="ev" :data-testid="`event-${e.kind}`">
          <span :class="['dot', e.category]" />
          <div class="main">
            <div class="line">
              <strong>{{ kindLabel(e.kind) }}</strong>
              <code v-if="e.target" class="target">{{ e.target }}</code>
              <span v-if="e.count > 1" class="badge" :title="t('activity.last', { date: when(e.updated_at) })">{{ t('activity.count', { n: e.count }) }}</span>
            </div>
            <div class="meta note">
              <span>{{ when(e.created_at) }}</span>
              <span v-if="e.count > 1">· {{ t('activity.last', { date: when(e.updated_at) }) }}</span>
              <span>· {{ e.ip ? t('activity.ip', { ip: e.ip }) : t('activity.unknownIp') }}</span>
              <span v-if="agentLabel(e.user_agent)" :title="e.user_agent">· {{ agentLabel(e.user_agent) }}</span>
            </div>
          </div>
        </li>
      </ul>
      <n-button v-if="next" :loading="loading" data-testid="activity-more" @click="load(true)">{{ t('activity.loadMore') }}</n-button>
    </section>
    <p class="note">{{ t('activity.retention') }}</p>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 16px;
  max-width: 900px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.card {
  padding: 16px 22px 20px;
  display: grid;
  gap: 12px;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.events {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
}

.ev {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 11px 0;
  border-bottom: 1px solid var(--border);
}

.dot {
  flex: none;
  width: 10px;
  height: 10px;
  margin-top: 6px;
  border-radius: 50%;
  background: #94a3b8;
}

.dot.security {
  background: #f43f5e;
}

.dot.sites {
  background: #22d3ee;
}

.dot.access {
  background: #fbbf24;
}

.dot.services {
  background: #a78bfa;
}

.main {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.line {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: baseline;
}

.target {
  font-size: 12.5px;
  word-break: break-all;
}

.badge {
  padding: 1px 9px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  background: rgba(148, 163, 184, 0.2);
}

.meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
</style>
