<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiSheet from '@/ui/UiSheet.vue'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()

// Код берём один раз при открытии экрана: после подтверждения он стирается из памяти.
const code = ref(auth.pendingBackupCode)
const saved = ref(false)
const copied = ref('')

const fileText = computed(
  () =>
    `${t('backup.fileTitle')}\n\n${t('backup.fileLogin')}: ${auth.user?.login ?? ''}\n${t('backup.fileCode')}: ${code.value}\n\n` +
    `${t('backup.fileNote1')}\n${t('backup.fileNote2')}\n`,
)

async function copy() {
  try {
    await navigator.clipboard.writeText(code.value)
    copied.value = t('backup.copied')
  } catch {
    copied.value = t('backup.copyFailed')
  }
}

function download() {
  const url = URL.createObjectURL(new Blob([fileText.value], { type: 'text/plain;charset=utf-8' }))
  const a = document.createElement('a')
  a.href = url
  a.download = 'kupol-backup-code.txt'
  a.click()
  URL.revokeObjectURL(url)
}

function proceed() {
  auth.acknowledgeBackupCode()
  code.value = ''
  void router.push({ name: 'file' })
}

// Пока код не подтверждён, случайное закрытие вкладки его безвозвратно потеряет — предупреждаем.
function warnBeforeLeaving(event: BeforeUnloadEvent) {
  if (!saved.value) {
    event.preventDefault()
    event.returnValue = ''
  }
}
onMounted(() => window.addEventListener('beforeunload', warnBeforeLeaving))
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnBeforeLeaving))
</script>

<template>
  <UiSheet as="article" class="backup">
    <h1>{{ $t('backup.title') }}</h1>
    <p>
      <i18n-t keypath="backup.intro" scope="global"><template #once><strong>{{ $t('backup.once') }}</strong></template></i18n-t>
    </p>

    <p id="backup-code-label" class="label">{{ $t('backup.yourCode') }}</p>
    <p class="code" aria-labelledby="backup-code-label" data-testid="backup-code">{{ code }}</p>

    <div class="actions">
      <UiButton icon="cards" @click="copy">{{ $t('common.copy') }}</UiButton>
      <UiButton icon="file" @click="download">{{ $t('common.downloadFile') }}</UiButton>
    </div>
    <UiAlert v-if="copied" tone="info" class="copied">{{ copied }}</UiAlert>

    <div class="confirm">
      <UiCheckbox v-model="saved" :label="$t('common.savedCodes')" />
    </div>
    <UiButton variant="primary" :disabled="!saved" icon-end="check" @click="proceed">{{ $t('backup.proceed') }}</UiButton>
  </UiSheet>
</template>

<style scoped>
.backup {
  max-width: 40rem;
  margin-top: var(--space-4);
}
.label {
  margin: var(--space-5) 0 var(--space-1);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.code {
  margin: 0 0 var(--space-4);
  padding: var(--space-4);
  border: 2px dashed var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
  font-family: var(--font-doc);
  font-size: var(--text-xl);
  font-weight: 700;
  letter-spacing: 0.06em;
  overflow-wrap: anywhere;
  user-select: all;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.copied {
  margin-bottom: var(--space-3);
}
.confirm {
  margin: var(--space-4) 0;
}
</style>
