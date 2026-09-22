<script setup lang="ts">
import { computed, ref } from 'vue'
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
import { t, tc } from '@/i18n'
import { levelName } from '@/lib/levels'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()
const router = useRouter()
const logoutError = ref('')
const loggingOut = ref(false)

// Полоса опыта: доля пути до следующего уровня (next_level_xp = 0, когда дальше только решением Особого Совета).
const xpProgress = computed(() => {
  const u = auth.user
  if (!u || u.next_level_xp <= 0) return 100
  return Math.min(100, Math.round((u.xp / u.next_level_xp) * 100))
})
const nextLevelText = computed(() => {
  const u = auth.user
  if (!u) return ''
  if (u.next_level_xp <= 0) return t('file.maxAutoLevel')
  return t('file.nextLevel', { level: u.level + 1, name: levelName(u.level + 1), left: Math.max(0, u.next_level_xp - u.xp) })
})
const streakText = computed(() => (auth.user ? tc('file.streakDays', auth.user.login_streak) : ''))

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
    <UiPageHeader :title="$t('file.title')" :kicker="$t('file.kicker')" />

    <div class="file__grid">
      <UiSheet as="article" class="file__card" :aria-label="$t('file.card')">
        <div class="file__head">
          <p class="file__level" aria-hidden="true">{{ auth.user.level }}</p>
          <UiStamp v-if="auth.user.directorate" :text="levelName(7)" :tilt="-4" />
        </div>
        <dl class="dossier">
          <div>
            <dt>{{ $t('file.nick') }}</dt>
            <dd data-testid="file-login">{{ auth.user.login }}</dd>
          </div>
          <div>
            <dt>{{ $t('file.rank') }}</dt>
            <dd data-testid="file-rank">{{ auth.user.level_name }}</dd>
          </div>
          <div v-if="!auth.user.directorate">
            <dt>{{ $t('file.level') }}</dt>
            <dd>{{ auth.user.level }}</dd>
          </div>
          <div v-if="!auth.user.directorate">
            <dt>{{ $t('file.xp') }}</dt>
            <dd data-testid="file-xp">
              {{ $t('file.xpValue', { xp: auth.user.xp }) }}
              <div class="xp-bar" role="progressbar" :aria-valuenow="xpProgress" aria-valuemin="0" aria-valuemax="100" :aria-label="$t('file.xp')">
                <div class="xp-bar__fill" :style="{ width: `${xpProgress}%` }" />
              </div>
              <p class="xp-next">{{ nextLevelText }}</p>
            </dd>
          </div>
          <div v-if="auth.user.login_streak > 0">
            <dt>{{ $t('file.streak') }}</dt>
            <dd data-testid="file-streak">{{ streakText }}</dd>
          </div>
          <div v-if="auth.user.roles.length">
            <dt>{{ $t('file.roles') }}</dt>
            <dd data-testid="file-roles">{{ auth.user.roles.map((r) => r.name).join(', ') }}</dd>
          </div>
          <div>
            <dt>{{ $t('file.joined') }}</dt>
            <dd>{{ formatDate(auth.user.created_at) }}</dd>
          </div>
        </dl>
      </UiSheet>

      <div class="file__side">
        <UiSheet as="section" aria-labelledby="pw-title">
          <h2 id="pw-title">{{ $t('file.passwordTitle') }}</h2>
          <ChangePasswordForm />
        </UiSheet>

        <UiSheet as="section" aria-labelledby="totp-title">
          <h2 id="totp-title">{{ $t('file.totpTitle') }}</h2>
          <TotpPanel />
        </UiSheet>

        <UiSheet as="section" aria-labelledby="session-title">
          <h2 id="session-title">{{ $t('file.sessionTitle') }}</h2>
          <UiAlert v-if="logoutError" tone="danger">{{ logoutError }}</UiAlert>
          <UiButton :loading="loggingOut" icon="logout" @click="logout">{{ loggingOut ? $t('file.loggingOut') : $t('file.logout') }}</UiButton>
        </UiSheet>

        <UiSheet as="div"><DeleteAccountForm /></UiSheet>
      </div>
    </div>
  </div>

  <UiSheet v-else as="article">
    <h1>{{ $t('file.title') }}</h1>
    <p>{{ $t('file.needLogin') }}</p>
    <UiButton variant="primary" @click="ui.openAuth()">{{ $t('file.signIn') }}</UiButton>
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
.xp-bar {
  margin-top: var(--space-2);
  height: 0.5rem;
  border: 1px solid var(--ink-900);
  border-radius: var(--radius-1);
  overflow: hidden;
  background: var(--paper-100);
}
.xp-bar__fill {
  height: 100%;
  background: var(--red-700);
}
.xp-next {
  margin: var(--space-1) 0 0;
  font-family: var(--font-head);
  font-size: var(--text-xs);
  font-weight: 400;
  letter-spacing: normal;
  text-transform: none;
  color: var(--text-muted);
}
@media (max-width: 56rem) {
  .file__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
