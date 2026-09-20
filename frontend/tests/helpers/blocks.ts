import type { InputBlock } from '@/api/generated/documents'
import canonicalJson from '../../e2e/fixtures/editor-blocks.json'

export const block = (type: string, data: unknown, extra: Partial<InputBlock> = {}): InputBlock => ({ id: `${type}1`, type, data, ...extra })

/**
 * Каноническая форма — та, что хранит сервер: её преобразование туда и обратно не меняет ни знака. Все 15 видов блоков.
 * Тот же файл берут и сквозные тесты (e2e/editor.spec.js): один образец на всё.
 */
export const canonical: InputBlock[] = canonicalJson as InputBlock[]
