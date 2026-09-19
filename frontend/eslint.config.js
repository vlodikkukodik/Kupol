import globals from 'globals'
import pluginVue from 'eslint-plugin-vue'

export default [
  { ignores: ['dist/**', 'node_modules/**', 'public/**'] },
  ...pluginVue.configs['flat/recommended'],
  {
    languageOptions: { ecmaVersion: 2023, sourceType: 'module', globals: { ...globals.browser, ...globals.node } },
    rules: {
      'vue/multi-word-component-names': 'off',
      // чисто стилистические переносы в шаблонах — не ошибки
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
    },
  },
]
