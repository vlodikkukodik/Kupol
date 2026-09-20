<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import type { Editor } from '@tiptap/core'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { deleteTableRow, insertBlock, insertBlocks, moveBlock, removeBlock, setBlockLevel, setRedact, type Situation } from '@/editor/commands'
import { INSERTABLE, NODE_LABELS } from '@/editor/fields'
import { LEVEL_NAMES, MAX_LEVEL } from '@/lib/levels'
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
    const skipped = res.skipped ? ` Шапка досье не вставлена: в документе она уже есть.` : ''
    emit('announce', res.ids.length ? `Вставлен набор «${tpl.name}»: блоков ${res.ids.length}.${skipped}` : `Набор «${tpl.name}» не вставлен.${skipped}`)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    emit('announce', 'Не удалось открыть набор блоков: возможно, его удалили.')
    void sets.refetch()
  }
}

const levelOptions = Array.from({ length: MAX_LEVEL }, (_, i) => ({ value: String(i + 1), label: `${i + 1} — ${LEVEL_NAMES[i + 1]}` }))

const blockName = computed(() => (props.state.block ? (NODE_LABELS[props.state.block.node] ?? props.state.block.node) : ''))
const blockPlace = computed(() => (props.state.block ? `${props.state.block.index + 1} из ${props.state.count}` : ''))

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
  run(level > 0 ? `Выделенный текст закрыт до уровня ${level}.` : 'Выделенный текст открыт.', ok)
}

function onInsert(event: Event) {
  const select = event.target as HTMLSelectElement
  const type = select.value
  select.value = ''
  if (!type) return
  const id = insertBlock(props.editor, type)
  if (!id) return
  emit('inserted', id)
  emit('announce', `Вставлен блок: ${INSERTABLE.find((i) => i.type === type)?.label ?? type}.`)
}

function onBlockLevel(event: Event) {
  const raw = (event.target as HTMLSelectElement).value
  const level = raw === '' ? null : Number(raw)
  const ok = setBlockLevel(props.editor, level)
  run(level === null ? 'Допуск блока — как у документа.' : `Блок закрыт до уровня ${level}.`, ok)
}

function move(dir: -1 | 1) {
  if (moveBlock(props.editor, dir)) {
    props.editor.commands.focus()
    emit('announce', dir < 0 ? 'Блок поднят выше.' : 'Блок опущен ниже.')
  }
}

function remove() {
  const name = blockName.value
  if (removeBlock(props.editor)) {
    props.editor.commands.focus()
    emit('announce', `Блок удалён: ${name}.`)
  }
}

function tableCommand(name: 'addRowAfter' | 'addColumnAfter' | 'deleteColumn' | 'deleteRow') {
  const ed = props.editor
  const ok = name === 'deleteRow' ? deleteTableRow(ed) : ed.chain().focus()[name]().run()
  const said = { addRowAfter: 'Строка добавлена.', addColumnAfter: 'Столбец добавлен.', deleteColumn: 'Столбец удалён.', deleteRow: 'Строка удалена.' }[name]
  run(said, ok)
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

const tools: { id: 'bold' | 'italic'; icon: IconName; label: string; command: 'toggleBold' | 'toggleItalic'; key: string }[] = [
  { id: 'bold', icon: 'bold', label: 'Жирный', command: 'toggleBold', key: 'Ctrl+B' },
  { id: 'italic', icon: 'italic', label: 'Курсив', command: 'toggleItalic', key: 'Ctrl+I' },
]
</script>

<template>
  <div ref="root" class="tb" role="toolbar" aria-label="Правка документа" data-testid="editor-toolbar" @keydown="onKeydown" @mousedown="onMousedown">
    <div class="tb__group" role="group" aria-label="Текст">
      <button
        v-for="t in tools"
        :key="t.id"
        type="button"
        class="tb__btn"
        :aria-pressed="state[t.id] ? 'true' : 'false'"
        :aria-label="t.label"
        :title="`${t.label} (${t.key})`"
        :disabled="!editable || !state.canFormat"
        :data-testid="`tb-${t.id}`"
        @click="toggle(t.command)"
      >
        <UiIcon :name="t.icon" />
      </button>
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="redact" size="1.1em" /> Закрыть до</span>
        <select
          class="tb__field"
          :value="String(state.redact)"
          :disabled="!editable || !state.canFormat"
          data-testid="tb-redact"
          aria-label="Закрыть выделенный текст до уровня"
          @change="onRedact"
        >
          <option value="0">не закрыт</option>
          <option v-for="o in levelOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
        </select>
      </label>
    </div>

    <div class="tb__group" role="group" aria-label="История правок">
      <button type="button" class="tb__btn" aria-label="Отменить" title="Отменить (Ctrl+Z)" :disabled="!editable || !state.canUndo" data-testid="tb-undo" @click="editor.chain().focus().undo().run()">
        <UiIcon name="undo" />
      </button>
      <button type="button" class="tb__btn" aria-label="Вернуть" title="Вернуть (Ctrl+Shift+Z)" :disabled="!editable || !state.canRedo" data-testid="tb-redo" @click="editor.chain().focus().redo().run()">
        <UiIcon name="redo" />
      </button>
    </div>

    <div class="tb__group" role="group" aria-label="Вставка">
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="plus" size="1.1em" /> Вставить</span>
        <select class="tb__field" :disabled="!editable" data-testid="tb-insert" aria-label="Вставить блок" @change="onInsert">
          <option value="">блок…</option>
          <option v-for="i in INSERTABLE" :key="i.type" :value="i.type" :disabled="i.node === 'dossierHeader' && hasDossierHeader">{{ i.label }} — {{ i.hint }}</option>
        </select>
      </label>
    </div>

    <div v-if="setsList.length" class="tb__group" role="group" aria-label="Наборы блоков">
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="layers" size="1.1em" /> Набор блоков</span>
        <select class="tb__field" :disabled="!editable" data-testid="tb-insert-set" aria-label="Вставить набор блоков" @change="onInsertSet">
          <option value="">выбрать…</option>
          <option v-for="t in setsList" :key="t.id" :value="t.id">{{ t.name }} — {{ t.blocks }}</option>
        </select>
      </label>
    </div>

    <div v-if="state.block" class="tb__group" role="group" :aria-label="`Текущий блок: ${blockName}, ${blockPlace}`">
      <span class="tb__place" aria-hidden="true">{{ blockName }} · {{ blockPlace }}</span>
      <button type="button" class="tb__btn" aria-label="Поднять блок выше" title="Поднять блок выше" :disabled="!editable || !state.canMoveUp" data-testid="tb-up" @click="move(-1)">
        <UiIcon name="arrow-up" />
      </button>
      <button type="button" class="tb__btn" aria-label="Опустить блок ниже" title="Опустить блок ниже" :disabled="!editable || !state.canMoveDown" data-testid="tb-down" @click="move(1)">
        <UiIcon name="arrow-down" />
      </button>
      <button type="button" class="tb__btn tb__btn--danger" aria-label="Удалить блок" title="Удалить блок" :disabled="!editable" data-testid="tb-remove" @click="remove">
        <UiIcon name="trash" />
      </button>
      <label class="tb__select">
        <span class="tb__cap"><UiIcon name="lock" size="1.1em" /> Допуск блока</span>
        <select class="tb__field" :value="state.block.level === null ? '' : String(state.block.level)" :disabled="!editable" data-testid="tb-block-level" aria-label="Допуск блока" @change="onBlockLevel">
          <option value="">как у документа</option>
          <option v-for="o in levelOptions" :key="o.value" :value="o.value">не ниже {{ o.label }}</option>
        </select>
      </label>
    </div>

    <div v-if="state.inTable" class="tb__group" role="group" aria-label="Таблица">
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-row-add" @click="tableCommand('addRowAfter')"><UiIcon name="rows" /> Строка +</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable || state.onTableHeader" :title="state.onTableHeader ? 'Строку заголовков удалить нельзя' : undefined" data-testid="tb-row-del" @click="tableCommand('deleteRow')"><UiIcon name="rows" /> Строка −</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-col-add" @click="tableCommand('addColumnAfter')"><UiIcon name="columns" /> Столбец +</button>
      <button type="button" class="tb__btn tb__btn--text" :disabled="!editable" data-testid="tb-col-del" @click="tableCommand('deleteColumn')"><UiIcon name="columns" /> Столбец −</button>
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
