import { expect } from '@playwright/test'
import { signUp } from './kupol.js'

// Общие помощники сквозных тестов панели команды: сотрудник с ролями и черновик, созданный настоящим запросом.

export async function newAuthor(browser, roles = ['author'], level = 4, directorate = false) {
  const context = await browser.newContext()
  const login = await signUp(context, { level, roles, directorate })
  const page = await context.newPage()
  return { context, page, login }
}

/** Создаёт черновик настоящим запросом (как это делает панель) и возвращает его номер. */
export async function createDoc(page, baseURL, { title = '[e2e] Редактор', blocks, ...extra }) {
  const res = await page.request.post('/api/team/documents', { headers: { Origin: new URL(baseURL).origin }, data: { type: 'object', code: '', title, composed: { year: 1979 }, blocks, ...extra } })
  expect(res.status(), await res.text()).toBe(201)
  return (await res.json()).document.id
}

export const stored = async (page, id) => (await (await page.request.get(`/api/team/documents/${id}`)).json()).document

/**
 * Курсор в конец текущей строки текста и ожидание, пока редактор это примет: он узнаёт о выделении браузера не мгновенно,
 * а Enter или набор в этот промежуток ушли бы в прежнее место. Живому человеку эта пауза незаметна.
 */
export async function caretToEnd(page) {
  await page.keyboard.press('End')
  await page.waitForFunction(() => {
    const { $from, empty } = document.querySelector('.ProseMirror').editor.state.selection
    return empty && $from.parentOffset === $from.parent.content.size
  })
}
