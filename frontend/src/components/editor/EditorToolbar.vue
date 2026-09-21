<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import type { Editor } from '@tiptap/core'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { deleteTableRow, insertBlock, insertBlocks, moveBlock, removeBlock, setBlockLevel, setRedact, type Situation } from '@/editor/commands'
import { INSERTABLE, insertHint, insertLabel, nodeLabel } from '@/editor/fields'
import { t } from '@/i18n'
import { levelName, MAX_LEVEL } from '@/lib/levels'
import UiIcon from '@/ui/UiIcon.vue'
import type { IconName } from '@/ui/icons'

// Панель инструментов: оформление текста, закрытие фрагмента, история, вставка блока, действия с текущим блоком и таблицей.
// Все элементы — обычные кнопки и выпадающие списки: работают с клавиатуры и на телефоне. Escape возвращает в текст.
const props = defineProps<{ editor: Editor; state: Situation; editable: boolean; hasDossierHeader: boolean }>()
const emit = defineEmits<{ announce: [text: string]; inserted: [id: string] }>()

const root = ref<HTMLElement | null>(null)
defineExpose({ focus: () => root.value?.querySelector<HTMLElement>('button:not([disabled]), select:not([disabled])')?.focus() })

// Наборы блоков из шаблонов: список общий на всё приложение (кэш), состав запрашивается при выборе
const sets = useQuery({ queryKey: keys.teamTemplates('blockset'), queryFn: ({ signal }) => teamApi.templates('blockset', { signal }), staleTime: 30_000, retry: false })
const setsList = computed(() => sets.data.value ?? [])

async function onInsertSet(event: Event) {
  const select = event.target as HTMLSelectElement
  const id = Number(select.value)
  select.value = ''
  if (!id) return
  try {
    const tpl = await teamApi.template(id)
    const res = insertBlocks(props.editor, tpl.content.blocks)
    if (res.ids[0]) emit('inserted', res.ids[0])
    const skipped = res.skipped ? t('editor.toolbar.say.dossierSkipped') : ''
    emit(
      'announce',
      (res.ids.length ? t('editor.toolbar.say.setInserted', { name: tpl.name, n: res.ids.length }) : t('editor.toolbar.say.setNotInserted', { name: tpl.name })) + skipped,
    )
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    emit('announce', t('editor.toolbar.say.setFailed'))
    void sets.refetch()
  }
}

const levelOptions = computed(() =>
  Array.from({ length: MAX_LEVEL }, (_, i) => ({ value: String(i + 1), label: t('editor.toolbar.levelOption', { level: i + 1, name: levelName(i + 1) }) })),
)

const blockName = computed(() => (props.state.block ? nodeLabel(props.state.block.node) : ''))
const blockPlace = computed(() => (props.state.block ? t('editor.toolbar.place', { n: props.state.block.index + 1, count: props.state.count }) : ''))

function run(text: string, ok: boolean) {
  if (ok) emit('announce', text)
}

function toggle(mark: 'toggleBold' | 'toggleItalic') {
  props.editor.chain().focus()[mark]().run()
}

function onRedact(event: Event) {
  const select = event.target as HTMLSelectElement
  const level = Number(select.value)
  const ok = setRedact(props.editor, level)
  run(level > 0 ? t('editor.toolbar.say.redacted', { level }) : t('editor.toolbar.say.notRedacted'), ok)
}

function onInsert(event: Event) {
  const select = event.target as HTMLSelectElement
  const type = select.value
  select.value = ''
  if (!type) return
  const id = insertBlock(props.editor, type)
  if (!id) return
  emit('inserted', id)
  const item = INSERTABLE.find((i) => i.type === type)
  emit('announce', t('editor.toolbar.say.inserted', { name: item ? insertLabel(item.node) : type }))
}

function onBlockLevel(event: Event) {
  const raw = (event.target as HTMLSelectElement).value
  const level = raw === '' ? null : Number(raw)
  const ok = setBlockLevel(props.editor, level)
  run(level === null ? t('editor.toolbar.say.blockAsDoc') : t('editor.toolbar.say.blockClosed', { level }), ok)
}

function move(dir: -1 | 1) {
  if (moveBlock(props.editor, dir)) {
    props.editor.commands.focus()
    emit('announce', dir < 0 ? t('editor.toolbar.say.up') : t('editor.toolbar.say.down'))
  }
}

function remove() {
  const name = blockName.value
  if (removeBlock(props.editor)) {
    props.editor.commands.focus()
    emit('announce', t('editor.toolbar.say.removed', { name }))
  }
}

function tableCommand(name: 'addRowAfter' | 'addColumnAfter' | 'deleteColumn' | 'deleteRow') {
  const ed = props.editor
  const ok = name === 'deleteRow' ? deleteTableRow(ed) : ed.chain().focus()[name]().run()
  const said = { addRowAfter: 'rowAdded', addColumnAfter: 'colAdded', deleteColumn: 'colDeleted', deleteRow: 'rowDeleted' }[name]
  run(t(`editor.toolbar.say.${said}`), ok)
}

/** Нажатие кнопки не должно уводить фокус (и выделение) из текста; выпадающие списки — обычные, им фокус нужен. */
function onMousedown(event: MouseEvent) {
  if ((event.target as HTMLElement).closest('button')) event.preventDefault()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    props.editor.commands.focus()
  }
}

const tools = computed<{ id: 'bold' | 'italic'; icon: IconName; label: string; command: 'toggleBold' | 'toggleItalic'; key: string }[]>(() => [
  { id: 'bold', icon: 'bold', label: t('editor.toolbar.bold'), command: 'toggleBold', key: 'Ctrl+B' },
  { id: 'italic', icon: 'italic', label: t('editor.toolbar.italic'), command: 'toggleItalic', key: 'Ctrl+I' },
])
</script>

<template>
  <div ref="root" class="tb" role="toolbar" :aria-label="$t('editor.toolbar.label')" data-testid="editor-toolbar" @keydown="onKeydown" @mousedown="onMousedown">
    <div class="tb__group" role="group" :aria-label="$t('editor.toolbar.text')">
      <button
        v-for="tool in tools"
        :key="tool.id"
        type="button"
        class="tb__btn"
        :aria-pressed="state[tool.id] ? 'true' : 'false'"
        :aria-label="tool.label"
        :title="`${tool.label} (${tool.key})`"
        :disabled="!editable || !state.canFormat"
        :data-testid="`tb-${tool.id}`"
        @click="toggle(tool.command)"
      >
        <UiIcon :name="tool.icon" />
      </button>
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="redact" size="1.1em" /> {{ $t('editor.toolbar.redactCap') }}</span>
        <select
          class="tb__field"
          :value="String(state.redact)"
          :disabled="!editable || !state.canFormat"
          data-testid="tb-redact"
          :aria-label="$t('editor.toolbar.redactLabel')"
          @change="onRedact"
        >
          <option value="0">{{ $t('editor.toolbar.notRedacted') }}</option>
          <option v-for="o in levelOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>
    </div>

    <div class="tb__group" role="group" :aria-label="$t('editor.toolbar.history')">
      <button type="button" class="tb__btn" :aria-label="$t('editor.toolbar.undo')" :title="$t('editor.toolbar.undoTitle')" :disabled="!editable || !state.canUndo" data-testid="tb-undo" @click="editor.chain().focus().undo().run()">
        <UiIcon name="undo" />
      </button>
      <button type="button" class="tb__btn" :aria-label="$t('editor.toolbar.redo')" :title="$t('editor.toolbar.redoTitle')" :disabled="!editable || !state.canRedo" data-testid="tb-redo" @click="editor.chain().focus().redo().run()">
        <UiIcon name="redo" />
      </button>
    </div>

    <div class="tb__group" role="group" :aria-label="$t('editor.toolbar.insertGroup')">
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="plus" size="1.1em" /> {{ $t('editor.toolbar.insertCap') }}</span>
        <select class="tb__field" :disabled="!editable" data-testid="tb-insert" :aria-label="$t('editor.toolbar.insertLabel')" @change="onInsert">
          <option value="">{{ $t('editor.toolbar.insertPlaceholder') }}</option>
          <option v-for="i in INSERTABLE" :key="i.type" :value="i.type" :disabled="i.node === 'dossierHeader' && hasDossierHeader">{{ $t('editor.toolbar.insertItem', { label: insertLabel(i.node), hint: insertHint(i.node) }) }}</option>
        </select>
      </label>
    </div>

    <div v-if="setsList.length" class="tb__group" role="group" :aria-label="$t('editor.toolbar.sets')">
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="layers" size="1.1em" /> {{ $t('editor.toolbar.setsCap') }}</span>
        <select class="tb__field" :disabled="!editable" data-testid="tb-insert-set" :aria-label="$t('editor.toolbar.setsLabel')" @change="onInsertSet">
          <option value="">{{ $t('editor.toolbar.setsPlaceholder') }}</option>
          <option v-for="s in setsList" :key="s.id" :value="s.id">{{ s.name }} — {{ s.blocks }}</option>
        </select>
      </label>
    </div>

    <div v-if="state.block" class="tb__group" role="group" :aria-label="$t('editor.toolbar.currentBlock', { name: blockName, place: blockPlace })">
      <span class="tb__place" aria-hidden="true">{{ blockName }} · {{ blockPlace }}</span>
      <button type="button" class="tb__btn" :aria-label="$t('editor.toolbar.up')" :title="$t('editor.toolbar.up')" :disabled="!editable || !state.canMoveUp" data-testid="tb-up" @click="move(-1)">
        <UiIcon name="arrow-up" />
      </button>
      <button type="button" class="tb__btn" :aria-label="$t('editor.toolbar.down')" :title="$t('editor.toolbar.down')" :disabled="!editable || !state.canMoveDown" data-testid="tb-down" @click="move(1)">
        <UiIcon name="arrow-down" />
      </button>
      <button type="button" class="tb__btn tb__btn--danger" :aria-label="$t('editor.toolbar.remove')" :title="$t('editor.toolbar.remove')" :disabled="!editable" data-testid="tb-remove" @click="remove">
        <UiIcon name="trash" />
      </button>
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="lock" size="1.1em" /> {{ $t('editor.toolbar.blockLevelCap') }}</span>
        <select class="tb__field" :value="state.block.level === null ? '' : String(state.block.level)" :disabled="!editable" data-testid="tb-block-level" :aria-label="$t('editor.toolbar.blockLevelLabel')" @change="onBlockLevel">
          <option value="">{{ $t('editor.toolbar.asDocument') }}</option>
          <option v-for="o in levelOptions" :key="o.value" :value="o.value">{{ $t('editor.toolbar.notBelow', { label: o.label }) }}</option>
        </select>
      </label>
    </div>

    <div v-if="state.inTable" class="tb__group" role="group" :aria-label="$t('editor.toolbar.table')">
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-row-add" @click="tableCommand('addRowAfter')"><UiIcon name="rows" /> {{ $t('editor.toolbar.rowAdd') }}</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable || state.onTableHeader" :title="state.onTableHeader ? $t('editor.toolbar.headerRowNoDelete') : undefined" data-testid="tb-row-del" @click="tableCommand('deleteRow')"><UiIcon name="rows" /> {{ $t('editor.toolbar.rowDel') }}</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-col-add" @click="tableCommand('addColumnAfter')"><UiIcon name="columns" /> {{ $t('editor.toolbar.colAdd') }}</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-col-del" @click="tableCommand('deleteColumn')"><UiIcon name="columns" /> {{ $t('editor.toolbar.colDel') }}</button>
    </div>
  </div>
</template>

<style scoped>
.tb {
  position: sticky;
  top: 0;
  z-index: 5;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-4);
  padding: var(--space-2) var(--space-3);
  border-bottom: 2px solid var(--ink-900);
  background: var(--surface-strong);
  font-family: var(--font-ui);
}
.tb__group {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}
.tb__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  min-width: var(--control-h);
  min-height: var(--control-h);
  padding: 0 var(--space-3);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
  color: var(--text);
  font-size: var(--text-sm);
  cursor: pointer;
}
.tb__btn:hover:not(:disabled) {
  border-color: var(--ink-900);
  background: var(--paper-0);
}
.tb__btn[aria-pressed='true'] {
  border-color: var(--ink-950);
  background: var(--ink-900);
  color: var(--paper-50);
}
.tb__btn--danger:hover:not(:disabled) {
  border-color: var(--danger);
  color: var(--danger);
}
.tb__btn:disabled {
  border-color: var(--border);
  background: transparent;
  color: var(--text-muted);
  cursor: not-allowed;
}
.tb__select {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}
.tb__cap {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  white-space: nowrap;
}
.tb__field {
  min-height: var(--control-h);
  max-width: 16rem;
  padding: 0 var(--space-3);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
  color: var(--text);
  font-family: var(--font-ui);
  font-size: var(--text-sm);
}
.tb__field:disabled {
  color: var(--text-muted);
  cursor: not-allowed;
}
.tb__place {
  max-width: 14rem;
  overflow: hidden;
  color: var(--text-muted);
  font-size: var(--text-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 40rem) {
  /* На телефоне панель — одна прокручиваемая строка: не съедает половину экрана */
  .tb {
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: thin;
  }
  .tb__group {
    flex-wrap: nowrap;
  }
}
</style>
