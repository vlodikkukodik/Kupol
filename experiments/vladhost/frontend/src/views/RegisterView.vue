<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput } from 'naive-ui'
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { fieldErrors, registerForm } from '@/api/schemas'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({
  invite: typeof route.query.invite === 'string' ? route.query.invite : '',
  email: '',
  username: '',
  password: '',
})
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

const hostPreview = computed(() => `сайт.${form.username.trim().toLowerCase() || 'имя'}.vladinc.ru`)

async function submit() {
  failure.value = ''
  const parsed = registerForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await auth.register(parsed.data)
    await router.push('/')
  } catch (e) {
    if (e instanceof ApiError && e.field) {
      errors.value = { [e.field]: e.message }
    } else {
      failure.value = e instanceof ApiError ? e.message : 'Не удалось зарегистрироваться'
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="center">
    <n-card title="Регистрация" class="card">
      <n-form @submit.prevent="submit">
        <n-form-item label="Код приглашения" :validation-status="errors.invite ? 'error' : undefined" :feedback="errors.invite">
          <n-input v-model:value="form.invite" autocomplete="off" :input-props="{ 'aria-label': 'Код приглашения' }" />
        </n-form-item>
        <n-form-item label="Email" :validation-status="errors.email ? 'error' : undefined" :feedback="errors.email">
          <n-input v-model:value="form.email" autocomplete="email" :input-props="{ 'aria-label': 'Email' }" />
        </n-form-item>
        <n-form-item
          label="Имя пользователя"
          :validation-status="errors.username ? 'error' : undefined"
          :feedback="errors.username ?? `Адреса сайтов: ${hostPreview}`"
        >
          <n-input v-model:value="form.username" autocomplete="username" :input-props="{ 'aria-label': 'Имя пользователя' }" />
        </n-form-item>
        <n-form-item label="Пароль" :validation-status="errors.password ? 'error' : undefined" :feedback="errors.password">
          <n-input v-model:value="form.password" type="password" show-password-on="click" autocomplete="new-password" :input-props="{ 'aria-label': 'Пароль' }" />
        </n-form-item>
        <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
        <n-button type="primary" attr-type="submit" block :loading="busy">Создать аккаунт</n-button>
      </n-form>
      <p class="hint"><router-link to="/login">Уже есть аккаунт</router-link></p>
    </n-card>
  </main>
</template>

<style scoped>
.center { min-height: 100vh; display: grid; place-items: center; padding: 16px; }
.card { width: 100%; max-width: 420px; }
.gap { margin-bottom: 12px; }
.hint { margin: 16px 0 0; font-size: 13px; opacity: 0.75; }
</style>
