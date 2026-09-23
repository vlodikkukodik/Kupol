<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput } from 'naive-ui'
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { fieldErrors, loginForm } from '@/api/schemas'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const form = reactive({ login: '', password: '' })
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

async function submit() {
  failure.value = ''
  const parsed = loginForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  busy.value = true
  try {
    await auth.login(parsed.data.login, parsed.data.password)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.push(next)
  } catch (e) {
    failure.value = e instanceof ApiError ? e.message : 'Не удалось войти'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="center">
    <n-card title="Вход в Vladhost" class="card">
      <n-form @submit.prevent="submit">
        <n-form-item label="Email или имя" :validation-status="errors.login ? 'error' : undefined" :feedback="errors.login">
          <n-input v-model:value="form.login" autofocus autocomplete="username" placeholder="john" :input-props="{ 'aria-label': 'Email или имя' }" />
        </n-form-item>
        <n-form-item label="Пароль" :validation-status="errors.password ? 'error' : undefined" :feedback="errors.password">
          <n-input v-model:value="form.password" type="password" show-password-on="click" autocomplete="current-password" :input-props="{ 'aria-label': 'Пароль' }" />
        </n-form-item>
        <n-alert v-if="failure" type="error" :show-icon="false" class="gap">{{ failure }}</n-alert>
        <n-button type="primary" attr-type="submit" block :loading="busy">Войти</n-button>
      </n-form>
      <p class="hint">Регистрация только по приглашению. <router-link to="/register">Есть код?</router-link></p>
    </n-card>
  </main>
</template>

<style scoped>
.center { min-height: 100vh; display: grid; place-items: center; padding: 16px; }
.card { width: 100%; max-width: 380px; }
.gap { margin-bottom: 12px; }
.hint { margin: 16px 0 0; font-size: 13px; opacity: 0.75; }
</style>
