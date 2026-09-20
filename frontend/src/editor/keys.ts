// Клавиши, которых нет у Tiptap «из коробки» и которые нужны блокам «Купола».
import { Extension } from '@tiptap/core'
import { exitEmptyListItem, insertLineBreak } from './commands'

/** Узлы, где текст — одна строка без деления на абзацы: Enter вставляет перенос строки. */
const LINE_BREAK_NODES = new Set(['quote', 'footnote', 'tableCell'])

export const KupolKeys = Extension.create({
  name: 'kupolKeys',
  // Раньше встроенных клавиш Tiptap: иначе Enter в ячейке или сноске сработает как деление блока.
  priority: 1000,
  addKeyboardShortcuts() {
    return {
      Enter: ({ editor }) => {
        if (exitEmptyListItem(editor)) return true
        const name = editor.state.selection.$from.parent.type.name
        if (LINE_BREAK_NODES.has(name)) return insertLineBreak(editor)
        if (name === 'tableHeader') return true // заголовок столбца — одна строка; перенос ему не нужен
        return false
      },
      'Shift-Enter': ({ editor }) => insertLineBreak(editor),
      'Mod-Enter': ({ editor }) => insertLineBreak(editor),
      Backspace: ({ editor }) => exitEmptyListItem(editor, { onlySingle: true }),
    }
  },
})
