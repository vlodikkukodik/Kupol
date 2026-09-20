import { computed, inject, provide, type ComputedRef, type InjectionKey } from 'vue'
import type { OutDocument } from '@/api/generated/documents'

// Шапка досье строится из свойств самого документа: блок берёт их отсюда, а не из своих данных.
const KEY: InjectionKey<ComputedRef<OutDocument | null>> = Symbol('document')

export const provideDocument = (doc: ComputedRef<OutDocument | null>) => provide(KEY, doc)

/** Документ, если текст рисуется внутри листа; иначе (например, в редакторе) — пустой ref: зависящее от документа просто не применяется. */
export function useDocumentOptional(): ComputedRef<OutDocument | null> {
  return inject(KEY, null) ?? computed(() => null)
}

export function useDocument(): ComputedRef<OutDocument | null> {
  const doc = inject(KEY)
  if (!doc) throw new Error('useDocument: блок рисуется вне документа')
  return doc
}
