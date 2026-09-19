<script setup lang="ts">
import { ref, useId } from 'vue'
import UiIcon from './UiIcon.vue'
import { useOverlay } from './overlay'

// Модальное окно: диалог с заголовком, крестиком, Esc и щелчком по полю вокруг. Что под ним — недоступно (inert
// ставит приложение на свою страницу, пока открыто хоть одно окно).
const open = defineModel<boolean>('open', { required: true })
const props = withDefaults(
  defineProps<{
    title: string
    /** Заголовок виден; иначе — только для скринридера (окно называет своё содержимое само) */
    showTitle?: boolean
    size?: 'sm' | 'md' | 'lg'
    initialFocus?: () => HTMLElement | null | undefined
    fallbackFocus?: () => HTMLElement | null
    testid?: string
    scrimTestid?: string
    /** Подпись кнопки-крестика (по умолчанию «Закрыть: <заголовок>») */
    closeLabel?: string
  }>(),
  { showTitle: true, size: 'md', testid: undefined, scrimTestid: undefined, closeLabel: undefined, initialFocus: undefined, fallbackFocus: undefined },
)
const emit = defineEmits<{ close: [] }>()

const panel = ref<HTMLElement | null>(null)
const titleId = `modal-title-${useId()}`
function close() {
  open.value = false
  emit('close')
}
useOverlay({ active: () => open.value, container: panel, onClose: close, initialFocus: props.initialFocus, fallbackFocus: props.fallbackFocus })
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-modal">
      <!-- .self: щелчок по полю вокруг окна закрывает его, щелчок внутри — нет -->
      <div v-if="open" class="ui-modal" @click.self="close">
        <div class="ui-modal__scrim" :data-testid="scrimTestid" @click="close" />
        <div
          ref="panel"
          class="ui-modal__panel"
          :class="`ui-modal__panel--${size}`"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          tabindex="-1"
          :data-testid="testid"
        >
          <header class="ui-modal__head">
            <h2 :id="titleId" class="ui-modal__title" :class="{ 'visually-hidden': !showTitle }">{{ title }}</h2>
            <button type="button" class="ui-modal__close" :aria-label="closeLabel ?? `Закрыть: ${title}`" @click="close">
              <UiIcon name="close" size="1.4rem" />
            </button>
          </header>
          <div class="ui-modal__body"><slot /></div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-modal {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: var(--space-5) var(--space-4);
  overflow-y: auto;
}
.ui-modal__scrim {
  position: fixed;
  inset: 0;
  background: rgb(6 9 8 / 0.72);
  backdrop-filter: blur(2px);
}
.ui-modal__panel {
  position: relative;
  width: 100%;
  margin-block: auto;
  padding: var(--space-5);
  border: 1px solid var(--paper-400);
  border-radius: var(--radius-2);
  background: var(--texture-paper), var(--surface);
  color: var(--text);
  box-shadow: var(--shadow-sheet);
}
.ui-modal__panel:focus {
  outline: none;
}
.ui-modal__panel--sm { max-width: 26rem; }
.ui-modal__panel--md { max-width: 34rem; }
.ui-modal__panel--lg { max-width: 48rem; }
.ui-modal__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.ui-modal__title {
  margin: 0;
}
.ui-modal__close {
  flex: none;
  display: grid;
  place-items: center;
  width: var(--control-h);
  height: var(--control-h);
  margin: calc(-1 * var(--space-2)) calc(-1 * var(--space-2)) 0 0;
  border: 2px solid transparent;
  border-radius: var(--radius-2);
  background: transparent;
  color: var(--text);
  cursor: pointer;
}
.ui-modal__close:hover {
  border-color: var(--ink-900);
  background: var(--ink-900);
  color: var(--paper-50);
}

.ui-modal-enter-active,
.ui-modal-leave-active {
  transition: opacity var(--dur) var(--ease);
}
.ui-modal-enter-active .ui-modal__panel,
.ui-modal-leave-active .ui-modal__panel {
  transition: transform var(--dur) var(--ease);
}
.ui-modal-enter-from,
.ui-modal-leave-to {
  opacity: 0;
}
.ui-modal-enter-from .ui-modal__panel,
.ui-modal-leave-to .ui-modal__panel {
  transform: translateY(-0.75rem) scale(0.985);
}
@media (max-width: 34rem) {
  .ui-modal { padding: var(--space-3); }
  .ui-modal__panel { padding: var(--space-4); }
}
</style>
