<script setup lang="ts">
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { bracketMatching, indentOnInput, syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { drawSelection, EditorView, highlightActiveLine, keymap, lineNumbers } from '@codemirror/view'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { langFor, languageExtension } from '@/lib/language'

const props = defineProps<{ modelValue: string; filename: string }>()
const emit = defineEmits<{ 'update:modelValue': [string]; save: [] }>()

const host = ref<HTMLDivElement>()
let view: EditorView | undefined

function build(doc: string): EditorState {
  return EditorState.create({
    doc,
    extensions: [
      lineNumbers(),
      history(),
      drawSelection(),
      indentOnInput(),
      bracketMatching(),
      highlightActiveLine(),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      oneDark,
      EditorView.lineWrapping,
      languageExtension(langFor(props.filename)),
      keymap.of([
        { key: 'Mod-s', preventDefault: true, run: () => (emit('save'), true) },
        indentWithTab,
        ...defaultKeymap,
        ...historyKeymap,
      ]),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
      }),
    ],
  })
}

onMounted(() => {
  view = new EditorView({ state: build(props.modelValue), parent: host.value })
})

// Внешнее изменение (открыли другой файл, откат) пересоздаёт состояние; собственные правки — нет.
watch(
  () => [props.filename, props.modelValue] as const,
  ([name, value], [oldName]) => {
    if (!view) return
    if (name !== oldName || value !== view.state.doc.toString()) view.setState(build(value))
  },
)

onBeforeUnmount(() => view?.destroy())
</script>

<template>
  <div ref="host" class="editor" />
</template>

<style scoped>
.editor { border: 1px solid #333; border-radius: 4px; overflow: hidden; }
.editor :deep(.cm-editor) { height: 60vh; }
.editor :deep(.cm-scroller) { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
</style>
