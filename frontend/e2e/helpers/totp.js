import { createHmac } from 'node:crypto'

// Код из приложения по RFC 6238 (SHA-1, 6 цифр, 30 секунд) — независимая от сервера запись: тест ничего не берёт у проверяемого кода.

const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

function base32Decode(text) {
  let bits = ''
  for (const ch of text.replace(/[\s=]/g, '').toUpperCase()) {
    const v = ALPHABET.indexOf(ch)
    if (v < 0) throw new Error(`не base32: ${ch}`)
    bits += v.toString(2).padStart(5, '0')
  }
  const bytes = []
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(bytes)
}

/** Код для секрета (base32, пробелы можно) на текущем шаге; offset — сдвиг в шагах по 30 секунд (+1 — следующий, допустим серверу). */
export function totpCode(secret, offset = 0) {
  const step = BigInt(Math.floor(Date.now() / 30000) + offset)
  const msg = Buffer.alloc(8)
  msg.writeBigUInt64BE(step)
  const sum = createHmac('sha1', base32Decode(secret)).update(msg).digest()
  const off = sum[sum.length - 1] & 0x0f
  const bin = sum.readUInt32BE(off) & 0x7fffffff
  return String(bin % 1_000_000).padStart(6, '0')
}
