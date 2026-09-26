import { css } from '@codemirror/lang-css'
import { html } from '@codemirror/lang-html'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'
import type { Extension } from '@codemirror/state'

export type LangId = 'html' | 'css' | 'js' | 'ts' | 'json' | 'markdown' | 'text'

const byExt: Record<string, LangId> = {
  html: 'html', htm: 'html', svg: 'html', xml: 'html',
  css: 'css',
  js: 'js', mjs: 'js', cjs: 'js', jsx: 'js',
  ts: 'ts', tsx: 'ts',
  json: 'json', webmanifest: 'json',
  md: 'markdown', markdown: 'markdown',
}

export function langFor(filename: string): LangId {
  const dot = filename.lastIndexOf('.')
  if (dot < 0) return 'text'
  return byExt[filename.slice(dot + 1).toLowerCase()] ?? 'text'
}

export function languageExtension(id: LangId): Extension[] {
  switch (id) {
    case 'html': return [html()]
    case 'css': return [css()]
    case 'js': return [javascript({ jsx: true })]
    case 'ts': return [javascript({ typescript: true, jsx: true })]
    case 'json': return [json()]
    case 'markdown': return [markdown()]
    default: return []
  }
}
