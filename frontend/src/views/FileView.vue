<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import ChangePasswordForm from '../components/ChangePasswordForm.vue'
import DeleteAccountForm from '../components/DeleteAccountForm.vue'
import Stamp from '../components/Stamp.vue'
import { describeApiError } from '../composables/useForm.js'
import { ApiError } from '../api/client.js'
import { formatDate } from '../lib/format.js'
import { useAuthStore } from '../stores/auth.js'
import { useUiStore } from '../stores/ui.js'

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
  <article v-if="auth.user" class="file">
    <div class="head">
      <h1>Личное дело</h1>
      <Stamp v-if="auth.user.directorate" text="Директорат" :tilt="-4" />
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

    <section class="block" aria-labelledby="pw-title">
      <h2 id="pw-title">Смена пароля</h2>
      <ChangePasswordForm />
    </section>

    <section class="block" aria-labelledby="session-title">
      <h2 id="session-title">Сеанс</h2>
      <p v-if="logoutError" class="form-error" role="alert">{{ logoutError }}</p>
      <button type="button" class="btn" :disabled="loggingOut" @click="logout">
        {{ loggingOut ? 'Выход…' : 'Выйти' }}
      </button>
    </section>

    <DeleteAccountForm />
  </article>

  <article v-else class="file">
    <h1>Личное дело</h1>
    <p>Чтобы открыть личное дело, нужно войти.</p>
    <p><button type="button" class="btn" @click="ui.openAuth()">Войти</button></p>
  </article>
</template>

<style scoped>
.head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); }
.dossier { margin: 0 0 var(--space-5); padding: 0; }
.dossier div {
  display: grid;
  grid-template-columns: minmax(9rem, 12rem) 1fr;
  gap: var(--space-3);
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--rule);
}
.dossier dt {
  font-family: var(--font-head);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-soft);
}
.dossier dd { margin: 0; font-weight: 700; overflow-wrap: anywhere; }
.block { margin-top: var(--space-5); padding-top: var(--space-4); border-top: 2px solid var(--ink); }
@media (max-width: 34rem) {
  .dossier div { grid-template-columns: 1fr; gap: 0; }
}
</style>
