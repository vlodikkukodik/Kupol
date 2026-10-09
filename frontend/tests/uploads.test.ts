// Загрузка в Team Files (этап 6.1): файл уходит на абсолютный адрес из билета — прямо на api-поддомин,
// минуя PHP-прокси (его php://input у multipart остаётся пустым, и Go не видит тело вообще).
// И причина отказа по полю файла должна быть видна у поля, а не общим «проверьте поля».
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import TeamUploadsView from '@/views/team/TeamUploadsView.vue'
import { uploadsApi } from '@/api/endpoints'
import type * as endpoints from '@/api/endpoints'
import { ApiError } from '@/api/client'

vi.mock('@/api/endpoints', async (importOriginal) => {
  const actual = await importOriginal<typeof endpoints>()
  return { ...actual, uploadsApi: { ...actual.uploadsApi, list: vi.fn(), upload: vi.fn(), remove: vi.fn() } }
})

const list = vi.mocked(uploadsApi.list)
const upload = vi.mocked(uploadsApi.upload)

const file = () => new File(['x'], 'схема.png', { type: 'image/png' })

let pinia: Pinia

beforeEach(() => {
  vi.clearAllMocks()
  pinia = createPinia()
  setActivePinia(pinia)
  list.mockResolvedValue([])
})

async function open(): Promise<VueWrapper> {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const w = mount(TeamUploadsView, { global: { plugins: [pinia, [VueQueryPlugin, { queryClient }]] } })
  await flushPromises()
  return w
}

/** Кладёт файл в <input type="file"> и отправляет форму. */
async function submit(w: VueWrapper) {
  const input = w.get('[data-testid="upload-file"]').element as HTMLInputElement
  Object.defineProperty(input, 'files', { value: [file()], configurable: true })
  await input.dispatchEvent(new Event('change', { bubbles: true }))
  await w.get('form').trigger('submit')
  await flushPromises()
}

describe('TeamUploadsView', () => {
  it('причина по полю файла видна у поля, а не общим текстом', async () => {
    upload.mockRejectedValue(
      new ApiError({
        status: 422,
        code: 'validation',
        message: 'Проверьте поля формы',
        fields: { file: 'Нужна картинка JPEG, PNG или WebP либо аудио mp3 или ogg' },
      }),
    )
    const w = await open()
    await submit(w)

    expect(w.text()).toContain('Нужна картинка JPEG, PNG или WebP')
    expect(w.text()).not.toContain('Проверьте поля формы')
  })

  it('ошибка без полей — общее предупреждение формы', async () => {
    upload.mockRejectedValue(new ApiError({ status: 422, code: 'validation', message: 'Проверьте поля формы' }))
    const w = await open()
    await submit(w)

    expect(w.text()).toContain('Проверьте поля формы')
  })
})

describe('uploadsApi.upload', () => {
  it('билет берётся через API, а файл уходит на абсолютный адрес из билета', async () => {
    const urls: string[] = []
    let putBody: unknown
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const url = String(input)
      const ticket = url.endsWith('/team/uploads/ticket')
      urls.push(url)
      if (!ticket) putBody = init.body
      const body = ticket
        ? { ticket: 'abc123', expires_at: '2026-01-01T00:00:00Z', path: '/api/uploads/put/abc123', url: 'https://api.example/api/uploads/put/abc123', max_bytes: 20 << 20 }
        : { upload: { id: 1, key: 'k'.repeat(32), kind: 'image', name: 'x.png', mime: 'image/webp', size: 3, level: 2, created_at: '2026-01-01T00:00:00Z' } }
      return new Response(JSON.stringify(body), {
        status: ticket ? 200 : 201,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    try {
      // настоящий uploadsApi (в файле замокан), чтобы проверить реальную отправку
      const real = await vi.importActual<typeof endpoints>('@/api/endpoints')
      const out = await real.uploadsApi.upload(file(), 2)
      expect(urls).toEqual(['/api/team/uploads/ticket', 'https://api.example/api/uploads/put/abc123'])
      expect(putBody).toBeInstanceOf(FormData)
      expect(out.key).toBe('k'.repeat(32))
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
