<script setup lang="ts">
import { computed, onBeforeUnmount, provide, ref, shallowRef, toRef, useId, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import { NodeSelection, Selection, TextSelection } from '@tiptap/pm/state'
import type { InputBlock } from '@/api/generated/documents'
import { hasNode, situation, type Situation } from '@/editor/commands'
import { EDITABLE } from '@/editor/context'
import { blocksToDoc, docToBlocks } from '@/editor/convert'
import { editorExtensions } from '@/editor/kit'
import { markProblemBlocks } from '@/editor/problems'
import EditorToolbar from './EditorToolbar.vue'
import '@/styles/editor.css'

// Редактор блоков документа. Содержимое читается один раз — при создании; дальше редактор ведёт его сам и после
// каждой правки отдаёт наружу блоки в серверном виде (update:blocks). Чтобы подменить содержимое (отмена правок,
// восстановление версии), родитель пересоздаёт компонент по :key — так не остаётся ни лишней истории отмены, ни гонок.
const props = withDefaults(defineProps<{ blocks: InputBlock[]; editable: boolean; problemBlocks?: string[] }>(), { problemBlocks: () => [] })
const emit = defineEmits<{ 'update:blocks': [blocks: InputBlock[]] }>()

const hintId = `editor-hint-${useId()}`
const announcement = ref('')
const situationNow = shallowRef<Situation | null>(null)
const dossier = ref(false)
const toolbar = ref<InstanceType<typeof EditorToolbar> | null>(null)

provide(EDITABLE, toRef(props, 'editable'))

let silent = true // пока редактор создаётся и приводит идентификаторы в порядок, наружу ничего не сообщаем

function refresh() {
  const ed = editor.value
  if (!ed) return
  situationNow.value = situation(ed)
  dossier.value = hasNode(ed.state, 'dossierHeader')
}

const editor = useEditor({
  extensions: editorExtensions(),
  content: blocksToDoc(props.blocks),
  editable: props.editable,
  editorProps: {
    attributes: {
      role: 'textbox',
      'aria-multiline': 'true',
      'aria-label': 'Текст документа',
      'aria-describedby': hintId,
      lang: 'ru',
    },
  },
  onCreate({ editor: ed }) {
    ed.commands.normalizeBlockIds()
    ed.commands.command(({ tr, state }) => {
      // Курсор — в первый текст, а не на первом блоке-«атоме»: иначе первая же клавиша заменила бы его
      const start = Selection.findFrom(state.doc.resolve(0), 1, true)
      if (start) tr.setSelection(start).setMeta('addToHistory', false)
      return Boolean(start)
    })
    silent = false
    refresh()
    if (props.problemBlocks.length) markProblemBlocks(ed, props.problemBlocks)
  },
  onUpdate({ editor: ed }) {
    if (!silent) emit('update:blocks', docToBlocks(ed.getJSON()))
  },
  onTransaction: refresh,
  onSelectionUpdate: refresh,
})

watch(
  () => props.editable,
  (value) => editor.value?.setEditable(value),
)
watch(
  () => props.problemBlocks,
  (ids) => {
    if (editor.value) markProblemBlocks(editor.value, ids)
  },
)
onBeforeUnmount(() => editor.value?.destroy())

/** Перейти к блоку по идентификатору: выделить и показать. Возвращает false, если такого блока нет. */
function focusBlock(id: string): boolean {
  const ed = editor.value
  if (!ed) return false
  let pos = -1
  ed.state.doc.forEach((node, offset) => {
    if (node.attrs.blockId === id) pos = offset
  })
  const node = pos >= 0 ? ed.state.doc.nodeAt(pos) : null
  if (!node) return false
  const tr = ed.state.tr
  tr.setSelection(node.isAtom ? NodeSelection.create(tr.doc, pos) : TextSelection.near(tr.doc.resolve(pos + 1)))
  ed.view.dispatch(tr)
  const field = node.isAtom ? (ed.view.nodeDOM(pos) as HTMLElement | null)?.querySelector<HTMLElement>('input, select, textarea') : null
  // У блока с полями фокус сразу в поле: сначала фокус в тексте отобрал бы его обратно при обновлении выделения
  if (!field) ed.commands.focus(null, { scrollIntoView: false })
  const dom = ed.view.nodeDOM(pos)
  if (dom instanceof HTMLElement) {
    dom.scrollIntoView({ block: 'center', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
    field?.focus({ preventScroll: true }) // чтобы сразу можно было заполнять
  }
  return true
}

/** Новый блок вставлен: у блока без текста (штамп, ссылка…) фокус — в первое поле, у остальных — в текст. */
function onInserted(id: string) {
  const ed = editor.value
  if (!ed) return
  const dom = ed.view.dom.querySelector<HTMLElement>(`[data-block-id="${id}"]`)
  let atom = false
  ed.state.doc.forEach((node) => {
    if (node.attrs.blockId === id) atom = node.isAtom
  })
  const field = atom ? dom?.querySelector<HTMLElement>('.pm-fields input, .pm-fields select, .pm-fields textarea') : null
  if (field) field.focus()
  else ed.commands.focus()
}

function onEditorKeydown(event: KeyboardEvent) {
  if (event.altKey && event.key === 'F10') {
    event.preventDefault()
    toolbar.value?.focus()
  }
}

const ready = computed(() => Boolean(editor.value && situationNow.value))
defineExpose({ focusBlock, editor })
</script>

<template>
  <div class="kupol-editor" :class="{ 'is-readonly': !editable }" data-testid="block-editor" @keydown.capture="onEditorKeydown">
    <EditorToolbar
      v-if="ready && editor && situationNow"
      ref="toolbar"
      :editor="editor"
      :state="situationNow"
      :editable="editable"
      :has-dossier-header="dossier"
      @announce="announcement = $event"
      @inserted="onInserted"
    />
    <p :id="hintId" class="visually-hidden">
      Редактор документа. Enter — новый абзац, Shift+Enter — перенос строки, Alt+F10 — панель инструментов, Escape на панели — назад в текст.
    </p>
    <p class="visually-hidden" role="status">{{ announcement }}</p>
    <EditorContent :editor="editor" class="kupol-editor__body" />
  </div>
</template>
