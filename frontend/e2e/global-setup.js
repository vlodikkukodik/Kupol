import { execFileSync } from 'node:child_process'
import { BACKEND, canWrite, removeDocuments, seedDocuments } from './helpers/kupol.js'

// Один раз перед всеми тестами: собрать CLI сервера и загрузить документы-фикстуры в БД сайта.
// После тестов global-teardown их удаляет. Против внешнего адреса без KUPOL_E2E_ALLOW_WRITES ничего не делается.
export default function globalSetup() {
  if (!canWrite) return
  execFileSync('go', ['build', '-o', 'bin/kupol', './cmd/kupol'], { cwd: BACKEND, stdio: 'inherit' })
  removeDocuments() // остатки прерванного прогона
  seedDocuments()
  return removeDocuments
}
