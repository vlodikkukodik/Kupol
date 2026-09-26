<script setup lang="ts">
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/i18n'

// Терминал в браузере: xterm.js поверх WebSocket. Двоичные кадры — данные терминала, текстовые — управление (изменение размера, итог).
const props = defineProps<{ ticket: string }>()
const emit = defineEmits<{ (e: 'closed'): void }>()
const { t } = useI18n()

const host = ref<HTMLElement | null>(null)
const status = ref<'connecting' | 'open' | 'ended'>('connecting')
const note = ref('')
let term: Terminal | undefined
let fit: FitAddon | undefined
let ws: WebSocket | undefined
let observer: ResizeObserver | undefined

const enc = new TextEncoder()

function sendSize() {
  if (!term || !ws || ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
}

function refusal(code: string): string {
  switch (code) {
    case 'disabled':
      return t('terminal.refused', { reason: t('terminal.disabled') })
    case 'busy':
      return t('terminal.refused', { reason: 'busy' })
    default:
      return t('terminal.refused', { reason: code })
  }
}

onMounted(() => {
  term = new Terminal({
    cursorBlink: true,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: 14,
    scrollback: 5000,
    theme: { background: '#070914', foreground: '#e5e7ff', cursor: '#a78bfa' },
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host.value!)
  fit.fit()

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${proto}//${location.host}/api/terminal/ws?ticket=${encodeURIComponent(props.ticket)}`)
  ws.binaryType = 'arraybuffer'
  ws.onopen = () => {
    status.value = 'open'
    sendSize()
    term?.focus()
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      try {
        const c = JSON.parse(ev.data) as { type: string; code?: number; error?: string }
        if (c.type === 'exit') note.value = t('terminal.ended', { code: c.code ?? 0 })
        else if (c.type === 'error') note.value = refusal(c.error ?? '')
      } catch {
        // управляющий кадр не разобрался — игнорируем
      }
      return
    }
    term?.write(new Uint8Array(ev.data as ArrayBuffer))
  }
  ws.onclose = () => {
    if (status.value !== 'ended') {
      status.value = 'ended'
      if (!note.value) note.value = t('terminal.lost')
      emit('closed')
    }
  }
  term.onData((d) => {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(enc.encode(d))
  })
  observer = new ResizeObserver(() => {
    fit?.fit()
    sendSize()
  })
  observer.observe(host.value!)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  ws?.close()
  term?.dispose()
})
</script>

<template>
  <div class="wrap">
    <div ref="host" class="term" data-testid="terminal" />
    <p v-if="status === 'connecting'" class="note">{{ t('terminal.opening') }}</p>
    <p v-if="note" class="note" data-testid="terminal-note">{{ note }}</p>
  </div>
</template>

<style scoped>
.wrap {
  display: grid;
  gap: 8px;
}

.term {
  height: 420px;
  padding: 10px;
  border-radius: 14px;
  background: #070914;
  border: 1px solid var(--border);
  overflow: hidden;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
}
</style>
