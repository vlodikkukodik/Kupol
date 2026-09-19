import { inject, provide, type InjectionKey } from 'vue'

/** Что UiField сообщает своему полю ввода: идентификатор, связь с подсказкой и ошибкой, признак ошибки. */
export interface FieldContext {
  id: string
  describedBy: string | undefined
  invalid: boolean
  required: boolean
}

const KEY: InjectionKey<FieldContext> = Symbol('ui-field')

export const provideField = (ctx: FieldContext) => provide(KEY, ctx)
export const useField = () => inject(KEY, null)
