<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiSheet from '@/ui/UiSheet.vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()

// Код берём один раз при открытии экрана: после подтверждения он стирается из памяти.
const code = ref(auth.pendingBackupCode)
const saved = ref(false)
const copied = ref('')

const fileText = computed(
  () =>
    `КУПОЛ — резервный код доступа\n\nЛогин: ${auth.user?.login ?? ''}\nКод: ${code.value}\n\n` +
    'Код показывается один раз. По нему можно восстановить доступ, если вы забудете пароль.\n' +
    'Храните его отдельно от пароля и никому не сообщайте.\n',
)

async function copy() {
  try {
    await navigator.clipboard.writeText(code.value)
    copied.value = 'Код скопирован'
  } catch {
    copied.value = 'Не удалось скопировать — выделите код и скопируйте вручную'
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
    <h1>Резервный код</h1>
    <p>
      Это единственный способ вернуть доступ, если вы забудете пароль: почты в архиве нет. Код показывается
      <strong>один раз</strong>. Сохраните его сейчас и храните отдельно от пароля.
    </p>

    <p id="backup-code-label" class="label">Ваш резервный код</p>
    <p class="code" aria-labelledby="backup-code-label" data-testid="backup-code">{{ code }}</p>

    <div class="actions">
      <UiButton icon="cards" @click="copy">Скопировать</UiButton>
      <UiButton icon="file" @click="download">Скачать файлом</UiButton>
    </div>
    <UiAlert v-if="copied" tone="info" class="copied">{{ copied }}</UiAlert>

    <div class="confirm">
      <UiCheckbox v-model="saved" label="Я сохранил(а) код в надёжном месте" />
    </div>
    <UiButton variant="primary" :disabled="!saved" icon-end="check" @click="proceed">Продолжить</UiButton>
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
