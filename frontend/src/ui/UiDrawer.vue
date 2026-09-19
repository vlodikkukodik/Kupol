<script setup lang="ts">
import { ref } from 'vue'
import UiIcon from './UiIcon.vue'
import { useOverlay } from './overlay'

// Боковая панель слева (меню). Ведёт себя как окно: фокус внутри, Esc и щелчок по затемнению закрывают.
const open = defineModel<boolean>('open', { required: true })
const props = withDefaults(
  defineProps<{
    label: string
    id?: string
    scrimTestid?: string
    closeLabel?: string
    fallbackFocus?: () => HTMLElement | null
  }>(),
  { id: undefined, scrimTestid: undefined, closeLabel: undefined, fallbackFocus: undefined },
)
const panel = ref<HTMLElement | null>(null)
function close() {
  open.value = false
}
useOverlay({ active: () => open.value, container: panel, onClose: close, fallbackFocus: props.fallbackFocus })
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-drawer">
      <div v-if="open" class="ui-drawer">
        <div class="ui-drawer__scrim" :data-testid="scrimTestid" @click="close" />
        <aside :id="id" ref="panel" class="ui-drawer__panel" role="dialog" aria-modal="true" :aria-label="label" tabindex="-1">
          <div class="ui-drawer__head">
            <slot name="head" />
            <button type="button" class="ui-drawer__close" :aria-label="closeLabel ?? `Закрыть: ${label}`" @click="close">
              <UiIcon name="close" size="1.4rem" />
            </button>
          </div>
          <slot />
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.ui-drawer {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay);
}
.ui-drawer__scrim {
  position: absolute;
  inset: 0;
  background: rgb(6 9 8 / 0.72);
  backdrop-filter: blur(2px);
}
.ui-drawer__panel {
  position: absolute;
  inset: 0 auto 0 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  width: min(21rem, 90vw);
  padding: var(--space-3) var(--space-4) var(--space-5);
  overflow-y: auto;
  border-right: 1px solid var(--paper-400);
  background: var(--texture-paper), var(--surface);
  color: var(--text);
  box-shadow: var(--shadow-pop);
}
.ui-drawer__panel:focus {
  outline: none;
}
.ui-drawer__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.ui-drawer__close {
  flex: none;
  display: grid;
  place-items: center;
  width: var(--control-h);
  height: var(--control-h);
  margin-left: auto;
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: transparent;
  color: var(--text);
  cursor: pointer;
}
.ui-drawer__close:hover {
  background: var(--ink-900);
  color: var(--paper-50);
}

.ui-drawer-enter-active,
.ui-drawer-leave-active {
  transition: opacity var(--dur) var(--ease);
}
.ui-drawer-enter-active .ui-drawer__panel,
.ui-drawer-leave-active .ui-drawer__panel {
  transition: transform var(--dur) var(--ease);
}
.ui-drawer-enter-from,
.ui-drawer-leave-to {
  opacity: 0;
}
.ui-drawer-enter-from .ui-drawer__panel,
.ui-drawer-leave-to .ui-drawer__panel {
  transform: translateX(-100%);
}
</style>
