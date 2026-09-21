// Общая настройка тестов: во всех смонтированных компонентах доступны $t и <i18n-t>, язык — русский (основной).
import { config } from '@vue/test-utils'
import { beforeEach } from 'vitest'
import { i18n } from '@/i18n'

config.global.plugins = [i18n]

// Язык одного теста не должен протекать в другой
beforeEach(() => {
  i18n.global.locale.value = 'ru'
})
