<script setup lang="ts">
import { AddOutline, CopyOutline, KeyOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NSpace, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { fieldErrors, sshKeyForm, sshKeyGeneratedSchema, sshKeyResponseSchema, sshKeysSchema, type SshKey } from '@/api/schemas'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'

const { t, locale } = useI18n()
const message = useMessage()

const keys = ref<SshKey[]>([])
const maxKeys = ref(10)
const hostFingerprint = ref('')
const loadError = ref('')
const busy = ref<number | null>(null)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    const r = await api('/api/ssh/keys', { schema: sshKeysSchema })
    keys.value = r.keys
    maxKeys.value = r.max_keys
    hostFingerprint.value = r.host_fingerprint
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('ssh.loadFailed'))
  }
}
onMounted(load)

const atLimit = computed(() => keys.value.length >= maxKeys.value)

// --- добавление своего ключа ---
const form = reactive({ name: '', publicKey: '' })
const errors = ref<Record<string, string>>({})
const adding = ref(false)

async function add() {
  const parsed = sshKeyForm.safeParse({ name: form.name, public_key: form.publicKey })
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  adding.value = true
  try {
    await api('/api/ssh/keys', { method: 'POST', body: parsed.data, schema: sshKeyResponseSchema })
    form.name = ''
    form.publicKey = ''
    message.success(t('ssh.added'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('ssh.addFailed')))
  } finally {
    adding.value = false
  }
}

// --- пара ключей из панели: закрытая часть показывается один раз ---
const generating = ref(false)
const privateKey = ref<{ name: string; pem: string } | null>(null)

async function generate() {
  if (!form.name.trim()) {
    errors.value = { name: t('ssh.nameInvalid') }
    return
  }
  generating.value = true
  try {
    const r = await api('/api/ssh/keys/generate', { method: 'POST', body: { name: form.name.trim() }, schema: sshKeyGeneratedSchema })
    privateKey.value = { name: r.key.name, pem: r.private_key }
    form.name = ''
    message.success(t('ssh.generated'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('ssh.generateFailed')))
  } finally {
    generating.value = false
  }
}

function download() {
  if (!privateKey.value) return
  const blob = new Blob([privateKey.value.pem], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'id_ed25519_vladhost'
  a.click()
  URL.revokeObjectURL(a.href)
}

async function remove(k: SshKey) {
  busy.value = k.id
  try {
    await api(`/api/ssh/keys/${k.id}`, { method: 'DELETE' })
    message.success(t('ssh.removed'))
    await load()
  } catch (e) {
    message.error(errText(e, t('ssh.removeFailed')))
  } finally {
    busy.value = null
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
      <h1>{{ t('ssh.title') }}</h1>
      <p class="note">{{ t('ssh.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>
    <p v-if="hostFingerprint" class="note">{{ t('ssh.hostKey') }}: <code>{{ hostFingerprint }}</code> — {{ t('ssh.hostKeyHint') }}</p>

    <section class="glass card rise" style="--i: 1">
      <p class="note">{{ t('ssh.limit', { used: keys.length, max: maxKeys }) }}</p>
      <p v-if="!keys.length && !loadError" class="note">{{ t('ssh.empty') }}</p>
      <ul v-if="keys.length" class="keys">
        <li v-for="k in keys" :key="k.id" class="key" :data-testid="`key-${k.name}`">
          <div class="krow">
            <span class="ic"><n-icon :size="16" :component="KeyOutline" /></span>
            <strong>{{ k.name }}</strong>
            <code class="algo">{{ k.algorithm }}</code>
            <span class="grow" />
            <n-popconfirm @positive-click="remove(k)">
              <template #trigger>
                <n-button size="small" class="tint-rose" :disabled="busy === k.id">{{ t('ssh.remove') }}</n-button>
              </template>
              {{ t('ssh.removeConfirm', { name: k.name }) }}
            </n-popconfirm>
          </div>
          <p class="note">
            {{ t('ssh.fingerprint') }}: <code>{{ k.fingerprint }}</code><br>
            {{ t('ssh.createdAt') }}: {{ formatDateTime(k.created_at, locale) }} · {{ t('ssh.lastUsed') }}:
            {{ k.last_used_at ? formatDateTime(k.last_used_at, locale) : t('ssh.never') }}
          </p>
        </li>
      </ul>
    </section>

    <section v-if="!atLimit" class="glass card rise" style="--i: 2">
      <n-form class="form" @submit.prevent="add">
        <n-form-item :label="t('ssh.name')" :validation-status="errors.name ? 'error' : undefined" :feedback="errors.name ? resolveMessage(errors.name) : undefined">
          <n-input v-model:value="form.name" :placeholder="t('ssh.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('ssh.name') }" @update:value="errors.name = ''" />
        </n-form-item>
        <n-form-item
          :label="t('ssh.publicKey')"
          :validation-status="errors.public_key ? 'error' : undefined"
          :feedback="errors.public_key ? resolveMessage(errors.public_key) : t('ssh.publicKeyHint')"
        >
          <n-input
            v-model:value="form.publicKey"
            type="textarea"
            :rows="3"
            :placeholder="t('ssh.publicKeyPlaceholder')"
            :input-props="{ 'aria-label': t('ssh.publicKey'), spellcheck: false }"
            @update:value="errors.public_key = ''"
          />
        </n-form-item>
        <n-space :size="10">
          <n-button type="primary" attr-type="submit" :loading="adding">
            <template #icon><n-icon :component="AddOutline" /></template>
            {{ t('ssh.add') }}
          </n-button>
          <n-button class="tint-violet" :loading="generating" @click="generate">{{ t('ssh.generate') }}</n-button>
        </n-space>
        <p class="note gen">{{ t('ssh.generateHint') }}</p>
      </n-form>
    </section>

    <n-modal :show="privateKey !== null" preset="card" :title="t('ssh.privateTitle')" style="max-width: 640px" :mask-closable="false" @update:show="privateKey = null">
      <template v-if="privateKey">
        <p class="note">{{ t('ssh.privateHint') }}</p>
        <pre class="pem" data-testid="private-key">{{ privateKey.pem }}</pre>
        <n-space :size="10">
          <n-button class="tint-violet" @click="copyText(privateKey.pem)">
            <template #icon><n-icon :component="CopyOutline" /></template>
            {{ t('common.copy') }}
          </n-button>
          <n-button class="tint-cyan" @click="download">{{ t('ssh.download') }}</n-button>
          <n-button type="primary" data-testid="private-close" @click="privateKey = null">{{ t('ssh.close') }}</n-button>
        </n-space>
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

.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.keys {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 14px;
}

.key {
  display: grid;
  gap: 4px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.krow {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.grow {
  flex: 1;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 9px;
  color: #fff;
  background: var(--grad-cyan);
}

.algo {
  font-size: 12px;
}

.form {
  max-width: 620px;
}

.gen {
  margin-top: 10px;
}

.pem {
  margin: 12px 0;
  padding: 12px;
  max-height: 260px;
  overflow: auto;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--border);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
