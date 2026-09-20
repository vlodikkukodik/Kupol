import { execFileSync } from 'node:child_process'
import { request } from '@playwright/test'
import { BACKEND, canWrite, removeDocuments, seedDocuments, signUp } from './helpers/kupol.js'

/**
 * Очередь на проверку общая для всей базы, а на рабочем столе видны только самые давние 20: документы, оставшиеся от прежних
 * прогонов, вытеснили бы из неё документы теста. Перед прогоном (пока параллельных тестов нет) очередь разбирается: Директорат
 * возвращает всё, что в ней лежит, авторам на доработку. Это те же вердикты, что и в интерфейсе, — никаких прямых правок базы.
 */
async function drainReviewQueue(baseURL) {
  const api = await request.newContext({ baseURL, extraHTTPHeaders: { Origin: new URL(baseURL).origin } })
  try {
    await signUp({ request: api }, { level: 6, directorate: true })
    for (let round = 0; round < 50; round++) {
      const desk = (await (await api.get('/api/team/dashboard')).json()).dashboard
      if (desk.queue.length === 0) return
      for (const item of desk.queue) {
        await api.post(`/api/team/documents/${item.id}/verdict`, { data: { verdict: 'return', comment: 'Очередь разобрана перед прогоном тестов.', base_revision: item.revision } })
      }
    }
  } finally {
    await api.dispose()
  }
}

// Один раз перед всеми тестами: собрать CLI сервера и загрузить документы-фикстуры в БД сайта.
// После тестов global-teardown их удаляет. Против внешнего адреса без KUPOL_E2E_ALLOW_WRITES ничего не делается.
export default async function globalSetup(config) {
  if (!canWrite) return
  execFileSync('go', ['build', '-o', 'bin/kupol', './cmd/kupol'], { cwd: BACKEND, stdio: 'inherit' })
  removeDocuments() // остатки прерванного прогона
  seedDocuments()
  await drainReviewQueue(config.projects[0].use.baseURL)
  return removeDocuments
}
