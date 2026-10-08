import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, unlinkSync, writeFileSync } from 'node:fs'

const DIR = '/tmp/vh-e2e-runtime'

/**
 * Роль исполнителя сред выполнения (на сервере это скрипт от root): забирает заявки из очереди и отвечает «успешно».
 * Возвращает таймер и список увиденных заявок «действие:среда:версия».
 */
export function startFakeHelper(seen: string[]): ReturnType<typeof setInterval> {
  return setInterval(() => {
    if (!existsSync(`${DIR}/queue`)) return
    for (const f of readdirSync(`${DIR}/queue`)) {
      if (!f.endsWith('.req')) continue
      const rid = f.slice(0, -4)
      const raw = readFileSync(`${DIR}/queue/${f}`, 'utf8')
      const field = (k: string) => new RegExp(`^${k}=(.*)$`, 'm').exec(raw)?.[1] ?? ''
      seen.push(`${field('action')}:${field('runtime')}:${field('version')}`)
      try {
        unlinkSync(`${DIR}/queue/${f}`)
      } catch {
        continue
      }
      // Доступ к оболочке: исполнитель ставит метку с адресом сайта и заводит папки, которые проверяет посредник оболочек.
      if (field('action') === 'shell-on') {
        mkdirSync(`/tmp/vh-e2e-sites/${field('host')}/tmp`, { recursive: true })
        mkdirSync(`${DIR}/shell`, { recursive: true })
        writeFileSync(`${DIR}/shell/${field('id')}`, field('host'))
      } else if (field('action') === 'shell-off' || field('action') === 'purge') {
        rmSync(`${DIR}/shell/${field('id')}`, { force: true })
      }
      // Журнал почты домена: исполнитель отдаёт JSON с событиями и очередью
      const mailLog = `{"events":[{"t":"2026-09-25 14:22:27","kind":"delivered","id":"1xA6ol-000000037ku-33mU","from":"friend@else.org","to":"info@${field('host')}","host":"","via":"mailbox","detail":"250 2.0.0 Saved"}],"queue":[]}\n`
      writeFileSync(`${DIR}/results/${rid}.out`, field('action') === 'logs' ? 'Server listening on 127.0.0.1\n' : field('action') === 'mail-log' ? mailLog : '')
      writeFileSync(`${DIR}/results/${rid}.res`, 'ok=1\nstate=active\n')
    }
  }, 50)
}
