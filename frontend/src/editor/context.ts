import type { InjectionKey, Ref } from 'vue'

/** Можно ли сейчас править документ: поля в шапках блоков (внутри представлений узлов) отключаются вместе с текстом. */
export const EDITABLE: InjectionKey<Ref<boolean>> = Symbol('kupol-editor-editable')
