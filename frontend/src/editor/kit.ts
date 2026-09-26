// Полный набор расширений редактора: схема документа + представления блоков + история и курсоры между блоками.
import type { AnyExtension } from '@tiptap/core'
import { Dropcursor, Gapcursor, TrailingNode, UndoRedo } from '@tiptap/extensions'
import { VueNodeViewRenderer } from '@tiptap/vue-3'
import type { Component } from 'vue'
import BlockNodeView from '@/components/editor/BlockNodeView.vue'
import TableNodeView from '@/components/editor/TableNodeView.vue'
import { KupolKeys } from './keys'
import { ProblemMarks } from './problems'
import { schemaExtensions } from './schema'

/** Узлы, у которых есть шапка с полями (остальные — обычный текст, ячейки таблицы, пункты). */
const VIEWS: Record<string, Component> = {
  heading: BlockNodeView,
  list: BlockNodeView,
  quote: BlockNodeView,
  dossierHeader: BlockNodeView,
  experimentLog: BlockNodeView,
  logEntry: BlockNodeView,
  stamp: BlockNodeView,
  memo: BlockNodeView,
  clipping: BlockNodeView,
  clipLine: BlockNodeView,
  docLink: BlockNodeView,
  divider: BlockNodeView,
  pageBreak: BlockNodeView,
  footnote: BlockNodeView,
  appendix: BlockNodeView,
  image: BlockNodeView,
  audio: BlockNodeView,
  unknownBlock: BlockNodeView,
  table: TableNodeView,
}

export function editorExtensions(): AnyExtension[] {
  const withViews = schemaExtensions.map((ext): AnyExtension => {
    const view = VIEWS[ext.name]
    if (!view) return ext
    return ext.extend({
      addNodeView() {
        return VueNodeViewRenderer(view)
      },
    })
  })
  return [
    ...withViews,
    KupolKeys,
    ProblemMarks,
    UndoRedo,
    Dropcursor.configure({ color: 'var(--red-600)', width: 3 }),
    Gapcursor,
    TrailingNode.configure({ node: 'paragraph' }),
  ]
}
