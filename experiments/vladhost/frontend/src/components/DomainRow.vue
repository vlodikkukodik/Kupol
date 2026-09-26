<script setup lang="ts">
import { CopyOutline, RefreshOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NPopconfirm } from 'naive-ui'
import { computed } from 'vue'
import type { Domain } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { useI18n } from '@/i18n'

// Строка имени сайта: свой домен или поддомен. Показывает статус, папку и доступные действия.
const props = defineProps<{ domain: Domain; serverIps: string[]; busy: boolean }>()
const emit = defineEmits<{ check: []; remove: []; editDir: []; copyIp: [ip: string] }>()

const { t } = useI18n()
const serverIp = computed(() => props.serverIps.join(', '))

const tone = { pending_dns: 'amber', pending_cert: 'cyan', active: 'emerald', failed: 'rose' } as const
const statusKey = {
  pending_dns: 'sites.domains.status.pendingDns',
  pending_cert: 'sites.domains.status.pendingCert',
  active: 'sites.domains.status.active',
  failed: 'sites.domains.status.failed',
} as const
const problemKey = {
  no_a: 'sites.domains.problem.noA',
  wrong_ip: 'sites.domains.problem.wrongIp',
  has_aaaa: 'sites.domains.problem.hasAaaa',
  lookup: 'sites.domains.problem.lookup',
} as const

const problem = computed(() => {
  const key = problemKey[props.domain.problem as keyof typeof problemKey]
  return key ? t(key, { found: props.domain.found.join(', '), ip: serverIp.value }) : ''
})

// Повторить можно то, что ждёт A-запись или не получило сертификат.
const canCheck = computed(() => props.domain.status === 'pending_dns' || props.domain.status === 'failed')
</script>

<template>
  <li class="dom-row">
    <div class="dom-main">
      <a v-if="domain.status === 'active'" :href="`https://${domain.host}`" target="_blank" rel="noopener" class="dom-host">{{ domain.host }}</a>
      <span v-else class="dom-host">{{ domain.host }}</span>
      <status-chip :tone="tone[domain.status]" :pulse="domain.status === 'pending_dns' || domain.status === 'pending_cert'">
        {{ t(statusKey[domain.status]) }}
      </status-chip>
      <status-chip tone="slate">{{ domain.dir ? t('subdomains.scopeDir', { dir: domain.dir }) : t('subdomains.scopeSite') }}</status-chip>
      <span class="grow" />
      <n-button v-if="canCheck" size="small" class="tint-cyan" :loading="busy" @click="emit('check')">
        <template #icon><n-icon :component="RefreshOutline" /></template>
        {{ domain.kind === 'sub' ? t('common.retry') : t('sites.domains.check') }}
      </n-button>
      <n-button size="small" class="tint-violet" :disabled="busy" @click="emit('editDir')">{{ t('subdomains.editDir') }}</n-button>
      <n-popconfirm @positive-click="emit('remove')">
        <template #trigger>
          <n-button size="small" class="tint-rose" :disabled="busy">{{ t('sites.domains.remove') }}</n-button>
        </template>
        {{ t('sites.domains.removeConfirm', { host: domain.host }) }}
      </n-popconfirm>
    </div>
    <template v-if="domain.status === 'pending_dns'">
      <div class="dom-todo">
        <code>{{ t('sites.domains.instruction', { host: domain.host, ip: serverIp }) }}</code>
        <n-button size="tiny" class="tint-violet" @click="emit('copyIp', serverIps[0] ?? '')">
          <template #icon><n-icon :component="CopyOutline" /></template>
          {{ t('sites.domains.copyIp') }}
        </n-button>
      </div>
      <p v-if="problem" class="dom-problem">{{ problem }}</p>
      <p class="note">{{ t('sites.domains.ttlNote') }}</p>
    </template>
    <p v-else-if="domain.status === 'failed'" class="dom-problem">
      {{ t('sites.domains.problem.failedBody', { reason: domain.error || t('sites.cert.unknownReason') }) }}
    </p>
  </li>
</template>

<style scoped>
.dom-row {
  padding: 10px 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.dom-main {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.dom-host {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.grow {
  flex: 1;
}

.dom-todo {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.dom-problem {
  margin: 8px 0 0;
  font-size: 13.5px;
  color: #fcd34d;
}

.note {
  margin: 8px 0 0;
  font-size: 13.5px;
  color: var(--text-dim);
}
</style>
