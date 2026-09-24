import { z } from 'zod'
import { api, apiVoid } from './client'

export const entrySchema = z.object({
  name: z.string(),
  is_dir: z.boolean(),
  size: z.number(),
  mod_time: z.string(),
})
export type FileEntry = z.infer<typeof entrySchema>

const htaccessSchema = z.object({
  files: z.array(
    z.object({
      path: z.string(),
      diags: z.array(z.object({ line: z.number(), directive: z.string(), code: z.string(), detail: z.string().optional() })),
    }),
  ),
})
export type HtaccessReport = z.infer<typeof htaccessSchema>['files']

const listSchema = z.object({ entries: z.array(entrySchema) })
const contentSchema = z.object({ content: z.string(), size: z.number() })

const q = (path: string) => `?path=${encodeURIComponent(path)}`
const base = (siteId: number) => `/api/sites/${siteId}`

export const filesApi = {
  async list(siteId: number, dir: string): Promise<FileEntry[]> {
    return (await api(`${base(siteId)}/files${q(dir)}`, { schema: listSchema })).entries
  },
  async htaccess(siteId: number): Promise<HtaccessReport> {
    return (await api(`${base(siteId)}/htaccess`, { schema: htaccessSchema })).files
  },
  async read(siteId: number, path: string): Promise<string> {
    return (await api(`${base(siteId)}/file${q(path)}`, { schema: contentSchema })).content
  },
  save(siteId: number, path: string, content: string): Promise<void> {
    return apiVoid(`${base(siteId)}/file${q(path)}`, { method: 'PUT', body: { content } })
  },
  mkdir(siteId: number, path: string): Promise<void> {
    return apiVoid(`${base(siteId)}/files/mkdir`, { method: 'POST', body: { path } })
  },
  rename(siteId: number, from: string, to: string): Promise<void> {
    return apiVoid(`${base(siteId)}/files/rename`, { method: 'POST', body: { from, to } })
  },
  remove(siteId: number, path: string): Promise<void> {
    return apiVoid(`${base(siteId)}/files${q(path)}`, { method: 'DELETE' })
  },
  upload(siteId: number, dir: string, file: File): Promise<void> {
    const body = new FormData()
    body.append('file', file)
    return apiVoid(`${base(siteId)}/files/upload${q(dir)}`, { method: 'POST', body })
  },
}
