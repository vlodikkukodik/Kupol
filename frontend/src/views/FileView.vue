<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import ChangePasswordForm from '@/components/ChangePasswordForm.vue'
import DeleteAccountForm from '@/components/DeleteAccountForm.vue'
import TotpPanel from '@/components/TotpPanel.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiStamp from '@/ui/UiStamp.vue'
import { ApiError } from '@/api/client'
import { describeApiError } from '@/composables/useForm'
import { formatDate } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()
const router = useRouter()
const logoutError = ref('')
const loggingOut = ref(false)

async function logout() {
  logoutError.value = ''
  loggingOut.value = true
  try {
    await auth.logout()
    await router.replace({ name: 'home' })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    logoutError.value = describeApiError(err)
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <div v-if="auth.user" class="file">
    <UiPageHeader title="Личное дело" kicker="Пропуск и сведения о допуске" />

    <div class="file__grid">
      <UiSheet as="article" class="file__card" aria-label="Карточка допуска">
        <div class="file__head">
          <p class="file__level" aria-hidden="true">{{ auth.user.level }}</p>
          <UiStamp v-if="auth.user.directorate" text="Директорат" :tilt="-4" />
        </div>
        <dl class="dossier">
          <div>
            <dt>Ник</dt>
            <dd data-testid="file-login">{{ auth.user.login }}</dd>
          </div>
          <div>
            <dt>Звание</dt>
            <dd data-testid="file-rank">{{ auth.user.level_name }}</dd>
          </div>
          <div v-if="!auth.user.directorate">
            <dt>Уровень допуска</dt>
            <dd>{{ auth.user.level }}</dd>
          </div>
          <div v-if="auth.user.roles.length">
            <dt>Роли команды</dt>
            <dd data-testid="file-roles">{{ auth.user.roles.map((r) => r.name).join(', ') }}</dd>
          </div>
          <div>
            <dt>Принят в архив</dt>
            <dd>{{ formatDate(auth.user.created_at) }}</dd>
          </div>
        </dl>
      </UiSheet>

      <div class="file__side">
        <UiSheet as="section" aria-labelledby="pw-title">
          <h2 id="pw-title">Смена пароля</h2>
          <ChangePasswordForm />
        </UiSheet>

        <UiSheet as="section" aria-labelledby="totp-title">
          <h2 id="totp-title">Код из приложения</h2>
          <TotpPanel />
        </UiSheet>

        <UiSheet as="section" aria-labelledby="session-title">
          <h2 id="session-title">Сеанс</h2>
          <UiAlert v-if="logoutError" tone="danger">{{ logoutError }}</UiAlert>
          <UiButton :loading="loggingOut" icon="logout" @click="logout">{{ loggingOut ? 'Выход…' : 'Выйти' }}</UiButton>
        </UiSheet>

        <UiSheet as="div"><DeleteAccountForm /></UiSheet>
      </div>
    </div>
  </div>

  <UiSheet v-else as="article">
    <h1>Личное дело</h1>
    <p>Чтобы открыть личное дело, нужно войти.</p>
    <UiButton variant="primary" @click="ui.openAuth()">Войти</UiButton>
  </UiSheet>
</template>

<style scoped>
.file__grid {
  display: grid;
  grid-template-columns: minmax(0, 20rem) minmax(0, 1fr);
  gap: var(--space-5);
  align-items: start;
}
.file__card {
  max-width: none;
  margin: 0;
}
.file__side {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-5);
}
.file__side > * {
  max-width: none;
  margin: 0;
}
.file__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.file__level {
  margin: 0;
  display: grid;
  place-items: center;
  width: 4.5rem;
  height: 4.5rem;
  border: 3px solid var(--ink-900);
  border-radius: var(--radius-2);
  font-family: var(--font-head);
  font-size: var(--text-4xl);
  font-weight: 700;
  line-height: 1;
}
.dossier {
  margin: 0;
}
.dossier > div {
  padding: var(--space-2) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.dossier dt {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.dossier dd {
  margin: 0;
  font-size: var(--text-lg);
  font-weight: 700;
  overflow-wrap: anywhere;
}
@media (max-width: 56rem) {
  .file__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
