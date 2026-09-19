<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'

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
  router.push({ name: 'file' })
}

// Пока код не подтверждён, случайное закрытие вкладки его безвозвратно потеряет — предупреждаем.
function warnBeforeLeaving(event) {
  if (!saved.value) {
    event.preventDefault()
    event.returnValue = ''
  }
}
onMounted(() => window.addEventListener('beforeunload', warnBeforeLeaving))
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnBeforeLeaving))
</script>

<template>
  <article class="backup">
    <h1>Резервный код</h1>
    <p>
      Это единственный способ вернуть доступ, если вы забудете пароль: почты в архиве нет. Код показывается
      <strong>один раз</strong>. Сохраните его сейчас и храните отдельно от пароля.
    </p>

    <p id="backup-code-label" class="code-label">Ваш резервный код</p>
    <p class="code" aria-labelledby="backup-code-label" data-testid="backup-code">{{ code }}</p>

    <div class="actions">
      <button type="button" class="btn" @click="copy">Скопировать</button>
      <button type="button" class="btn" @click="download">Скачать файлом</button>
    </div>
    <p v-if="copied" class="copied" role="status">{{ copied }}</p>

    <div class="check">
      <input id="backup-saved" v-model="saved" type="checkbox">
      <label for="backup-saved">Я сохранил(а) код в надёжном месте</label>
    </div>
    <button type="button" class="btn" :disabled="!saved" @click="proceed">Продолжить</button>
  </article>
</template>

<style scoped>
.code-label { margin: var(--space-4) 0 var(--space-1); font-family: var(--font-head); letter-spacing: 0.1em; text-transform: uppercase; }
.code {
  margin: 0 0 var(--space-3);
  padding: var(--space-3);
  border: 2px dashed var(--stamp-red);
  background: var(--paper-shade);
  color: var(--stamp-red);
  font-size: clamp(1.1rem, 4.2vw, 1.6rem);
  font-weight: 700;
  letter-spacing: 0.08em;
  overflow-wrap: anywhere;
  user-select: all;
}
.actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-bottom: var(--space-2); }
.copied { margin: 0 0 var(--space-3); font-size: 0.9rem; }
.check { display: flex; gap: var(--space-2); align-items: center; margin: var(--space-4) 0 var(--space-3); }
.check input { width: 1.2rem; height: 1.2rem; accent-color: var(--stamp-red); }
.check label { cursor: pointer; }
</style>
