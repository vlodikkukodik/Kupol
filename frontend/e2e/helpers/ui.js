import { expect } from '@playwright/test'

/** Дождаться конца всех анимаций и переходов на странице: меню выезжает, окно проявляется. */
export async function settle(page) {
  await page.evaluate(async () => {
    // Vue запускает переход на следующем кадре: сначала пропускаем два кадра, иначе анимаций ещё «нет».
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    await Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {})))
  })
}

/** Открыть боковое меню кнопкой-бургером и дождаться, пока оно полностью выедет. */
export async function openMenu(page) {
  await page.getByRole('button', { name: 'Меню', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Меню' })).toBeVisible()
  await settle(page)
}

/**
 * Открыть окно входа так, как это делает читатель: бургер -> «Войти или зарегистрироваться».
 * tab — 'Вход' (по умолчанию) или 'Регистрация'. Возвращает управление, когда меню уже убрано, а окно открыто.
 */
export async function openAuth(page, tab = 'Вход') {
  await openMenu(page)
  await page.getByRole('button', { name: 'Войти или зарегистрироваться' }).click()
  await expect(page.getByTestId('auth-modal')).toBeVisible()
  await expect(page.getByRole('dialog', { name: 'Меню' })).toHaveCount(0)
  await settle(page)
  if (tab !== 'Вход') await page.getByRole('tab', { name: tab }).click()
}
