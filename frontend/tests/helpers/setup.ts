// Общая настройка тестов: во всех смонтированных компонентах доступны $t и <i18n-t>, язык — русский (основной),
// и есть активная Pinia (компоненты вроде RedactedPlate читают auth/ui-стор, даже когда тест их не проверяет).
import { config } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach } from 'vitest'
import { i18n } from '@/i18n'

config.global.plugins = [i18n, createPinia()]

// Язык одного теста не должен протекать в другой
beforeEach(() => {
  i18n.global.locale.value = 'ru'
})
