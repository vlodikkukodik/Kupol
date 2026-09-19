<script setup>
import { nextTick, ref } from 'vue'
import LoginForm from './LoginForm.vue'
import RegisterForm from './RegisterForm.vue'

const tabs = [
  { id: 'login', label: 'Вход' },
  { id: 'register', label: 'Регистрация' },
]
const active = ref('login')
const tabEls = {}
const setTabRef = (id) => (el) => {
  tabEls[id] = el
}

/** Стрелки, Home и End переключают вкладки (шаблон ARIA Authoring Practices). */
async function onKeydown(event) {
  const keys = { ArrowRight: 1, ArrowLeft: -1 }
  let idx = tabs.findIndex((t) => t.id === active.value)
  if (event.key in keys) idx = (idx + keys[event.key] + tabs.length) % tabs.length
  else if (event.key === 'Home') idx = 0
  else if (event.key === 'End') idx = tabs.length - 1
  else return
  event.preventDefault()
  active.value = tabs[idx].id
  await nextTick()
  tabEls[active.value]?.focus()
}

</script>

<template>
  <section class="auth" aria-labelledby="auth-title">
    <h2 id="auth-title">Допуск в архив</h2>

    <div class="tablist" role="tablist" aria-label="Вход или регистрация" @keydown="onKeydown">
      <button
        v-for="t in tabs"
        :id="`tab-${t.id}`"
        :key="t.id"
        :ref="setTabRef(t.id)"
        type="button"
        role="tab"
        class="tab"
        :aria-selected="active === t.id ? 'true' : 'false'"
        :aria-controls="`panel-${t.id}`"
        :tabindex="active === t.id ? 0 : -1"
        @click="active = t.id"
      >
        {{ t.label }}
      </button>
    </div>

    <div id="panel-login" role="tabpanel" aria-labelledby="tab-login" :hidden="active !== 'login'">
      <LoginForm v-if="active === 'login'" />
    </div>
    <div id="panel-register" role="tabpanel" aria-labelledby="tab-register" :hidden="active !== 'register'">
      <RegisterForm v-if="active === 'register'" />
    </div>
  </section>
</template>

<style scoped>
.auth {
  margin-top: var(--space-5);
  padding: var(--space-4);
  border: 2px solid var(--ink);
  background: var(--paper-shade);
}
.auth h2 { margin-bottom: var(--space-3); }
.tablist { display: flex; gap: var(--space-1); margin-bottom: var(--space-4); border-bottom: 2px solid var(--ink); }
.tab {
  padding: 0.5rem 1.2rem;
  margin-bottom: -2px;
  border: 2px solid transparent;
  border-bottom: 0;
  background: transparent;
  color: var(--ink);
  font-family: var(--font-head);
  font-size: 1.05rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  cursor: pointer;
}
.tab[aria-selected='true'] {
  border-color: var(--ink);
  background: var(--paper);
  font-weight: 700;
}
.tab:hover:not([aria-selected='true']) { background: rgb(0 0 0 / 0.06); }
@media (max-width: 34rem) {
  .auth { padding: var(--space-3); }
  .tab { flex: 1; padding-inline: 0.4rem; }
}
</style>
