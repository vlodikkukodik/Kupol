import { createClient } from './client'

// VITE_API_BASE нужен только тестам (там нет относительных URL); в бою это /api.
export const api = createClient({ base: import.meta.env.VITE_API_BASE || '/api' })

export * from './client'
