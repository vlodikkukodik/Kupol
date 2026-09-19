<script setup>
import { ref } from 'vue'
import AuthPanel from './AuthPanel.vue'
import { useOverlay } from '../composables/useOverlay.js'
import { useUiStore } from '../stores/ui.js'

const ui = useUiStore()
const panel = ref(null)

useOverlay({
  active: () => ui.authOpen,
  container: panel,
  onClose: () => ui.closeAuth(),
  // сразу в поле логина: открыли окно, чтобы войти
  initialFocus: () => panel.value?.querySelector('input'),
  fallbackFocus: () => document.getElementById('menu-toggle'),
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <!-- .self: щелчок по полю вокруг окна закрывает его, щелчок внутри окна — нет -->
      <div v-if="ui.authOpen" class="modal-root" @click.self="ui.closeAuth()">
        <div class="scrim" data-testid="auth-scrim" @click="ui.closeAuth()" />
        <div ref="panel" class="modal" role="dialog" aria-modal="true" aria-labelledby="auth-title" tabindex="-1" data-testid="auth-modal">
          <button type="button" class="modal-close" aria-label="Закрыть окно входа" @click="ui.closeAuth()">
            <span aria-hidden="true">×</span>
          </button>
          <AuthPanel />
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-root {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: var(--space-4) var(--space-3);
  overflow-y: auto;
}
.scrim { position: fixed; inset: 0; background: rgb(0 0 0 / 0.65); }

.modal {
  position: relative;
  width: min(34rem, 100%);
  margin-block: auto;
  padding: var(--space-4);
  background: var(--paper);
  color: var(--ink);
  border: 2px solid var(--ink);
  box-shadow: var(--shadow-sheet);
}
.modal:focus { outline: none; }

/* Форма входа рисуется внутри окна: своей рамки и отступов у неё здесь нет. */
.modal :deep(section.auth) { margin: 0; padding: 0; border: 0; background: none; }
.modal :deep(.auth h2) { padding-right: 3.5rem; }

.modal-close {
  position: absolute;
  top: var(--space-2);
  right: var(--space-2);
  width: 2.75rem;
  height: 2.75rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: transparent;
  color: var(--ink);
  font-size: 1.8rem;
  line-height: 1;
  cursor: pointer;
}
.modal-close:hover { background: var(--ink); color: var(--paper); }

.modal-enter-active, .modal-leave-active { transition: opacity 0.18s ease; }
.modal-enter-active .modal, .modal-leave-active .modal { transition: transform 0.18s ease; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal, .modal-leave-to .modal { transform: translateY(-1rem); }

@media (max-width: 34rem) {
  .modal { padding: var(--space-3); }
}
</style>
