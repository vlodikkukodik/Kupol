// Блоки, к которым у сервера есть замечания: обводятся красным и помечаются как неверные (aria-invalid),
// чтобы автор сразу видел, что исправлять. Список блоков задаётся снаружи, после ответа сервера на сохранение.
import { Extension, type Editor } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'

const key = new PluginKey('kupolProblems')

export const ProblemMarks = Extension.create({
  name: 'problemMarks',
  addStorage() {
    return { ids: new Set<string>() }
  },
  addProseMirrorPlugins() {
    const storage = this.storage
    return [
      new Plugin({
        key,
        props: {
          decorations(state) {
            if (storage.ids.size === 0) return null
            const marks: Decoration[] = []
            state.doc.forEach((node, offset) => {
              if (storage.ids.has(String(node.attrs.blockId))) {
                marks.push(Decoration.node(offset, offset + node.nodeSize, { class: 'has-problem', 'aria-invalid': 'true' }))
              }
            })
            return DecorationSet.create(state.doc, marks)
          },
        },
      }),
    ]
  },
})

/** Пометить блоки с этими идентификаторами (пустой список — снять пометки). */
export function markProblemBlocks(editor: Editor, ids: string[]): void {
  const storage = (editor.storage as unknown as Record<string, { ids: Set<string> }>).problemMarks!
  storage.ids = new Set(ids)
  editor.view.dispatch(editor.state.tr.setMeta(key, true))
}

/** Номера блоков из путей замечаний сервера: «blocks[3].data.text» → 3. */
export function blockIndexes(paths: string[]): number[] {
  const seen = new Set<number>()
  for (const p of paths) {
    const m = /^blocks\[(\d+)\]/.exec(p)
    if (m) seen.add(Number(m[1]))
  }
  return [...seen].sort((a, b) => a - b)
}
