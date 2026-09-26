// Мелочи вокруг кода из приложения (TOTP): всё, что можно проверить без браузера.

/** Секрет группами по 4 знака: так его удобно вводить в приложение вручную («ABCD EFGH …»). */
export function groupSecret(secret: string): string {
  return secret.replace(/\s+/g, '').replace(/(.{4})(?=.)/g, '$1 ')
}
