// Краткое описание устройства по User-Agent для списка сессий: «Chrome · Windows». Точность не нужна — только узнать своё устройство.

export interface Device {
  browser: string // пусто — не распознан
  os: string
  mobile: boolean
}

const BROWSERS: [RegExp, string][] = [
  [/Edg(?:e|A|iOS)?\//, 'Edge'],
  [/OPR\/|Opera/, 'Opera'],
  [/YaBrowser\//, 'Yandex'],
  [/SamsungBrowser\//, 'Samsung Internet'],
  [/Firefox\/|FxiOS\//, 'Firefox'],
  [/Chrome\/|CriOS\//, 'Chrome'],
  [/Safari\//, 'Safari'],
  [/curl\//, 'curl'],
]

const SYSTEMS: [RegExp, string][] = [
  [/Windows/, 'Windows'],
  [/iPhone|iPad|iPod/, 'iOS'],
  [/Android/, 'Android'],
  [/Mac OS X|Macintosh/, 'macOS'],
  [/CrOS/, 'ChromeOS'],
  [/Linux/, 'Linux'],
]

export function parseUserAgent(ua: string): Device {
  const browser = BROWSERS.find(([re]) => re.test(ua))?.[1] ?? ''
  const os = SYSTEMS.find(([re]) => re.test(ua))?.[1] ?? ''
  return { browser, os, mobile: /Mobile|Android|iPhone|iPad/.test(ua) }
}
