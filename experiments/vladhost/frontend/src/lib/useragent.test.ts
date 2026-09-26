import { describe, expect, it } from 'vitest'
import { parseUserAgent } from './useragent'

describe('parseUserAgent', () => {
  it('узнаёт распространённые браузеры и системы', () => {
    const cases: [string, string, string, boolean][] = [
      ['Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36', 'Chrome', 'Windows', false],
      ['Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0', 'Edge', 'Windows', false],
      ['Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15', 'Safari', 'macOS', false],
      ['Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1', 'Safari', 'iOS', true],
      ['Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36', 'Chrome', 'Android', true],
      ['Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0', 'Firefox', 'Linux', false],
      ['Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 YaBrowser/25.1 Safari/537.36', 'Yandex', 'Windows', false],
    ]
    for (const [ua, browser, os, mobile] of cases) expect(parseUserAgent(ua)).toEqual({ browser, os, mobile })
  })

  it('не падает на пустой и незнакомой строке', () => {
    expect(parseUserAgent('')).toEqual({ browser: '', os: '', mobile: false })
    expect(parseUserAgent('something/1.0')).toEqual({ browser: '', os: '', mobile: false })
  })
})
