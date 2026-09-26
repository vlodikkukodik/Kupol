<script setup lang="ts">
import { ChevronBackOutline, SearchOutline } from '@vicons/ionicons5'
import { NButton, NIcon, NInput } from 'naive-ui'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import MarkdownView from '@/components/MarkdownView.vue'
import { articles, HELP_CATEGORIES, searchArticles, type HelpCategory } from '@/help/articles'
import { useI18n } from '@/i18n'

const { t, locale } = useI18n()
const route = useRoute()

const query = ref('')
const category = ref<HelpCategory | ''>('')

const all = computed(() => articles(locale.value))
const article = computed(() => all.value.find((a) => a.slug === route.params.slug) ?? null)
const shown = computed(() => searchArticles(all.value, query.value).filter((a) => !category.value || a.category === category.value))

// Названия разделов перечислены явно: сторож i18n ищет ключи в коде как строки.
const categoryLabel = (c: string) =>
  c === 'start'
    ? t('help.categories.start')
    : c === 'sites'
      ? t('help.categories.sites')
      : c === 'domains'
        ? t('help.categories.domains')
        : c === 'dns'
          ? t('help.categories.dns')
          : c === 'mail'
            ? t('help.categories.mail')
            : c === 'data'
              ? t('help.categories.data')
              : c === 'apps'
                ? t('help.categories.apps')
                : t('help.categories.access')
</script>

<template>
  <div class="page">
    <template v-if="route.params.slug">
      <router-link :to="{ name: 'help' }" class="back"><n-icon :component="ChevronBackOutline" /> {{ t('help.back') }}</router-link>
      <article v-if="article" class="glass card rise" data-testid="help-article">
        <span class="tag">{{ categoryLabel(article.category) }}</span>
        <h1>{{ article.title }}</h1>
        <p class="note lead">{{ article.description }}</p>
        <markdown-view :blocks="article.blocks" />
        <p class="ask">
          {{ t('help.noAnswer') }}
          <router-link :to="{ name: 'support' }">{{ t('help.askSupport') }}</router-link>
        </p>
      </article>
      <p v-else class="note" data-testid="help-missing">{{ t('help.notFound') }}</p>
    </template>

    <template v-else>
      <header class="head rise">
        <h1>{{ t('help.title') }}</h1>
        <p class="note">{{ t('help.hint') }}</p>
      </header>

      <n-input v-model:value="query" clearable data-testid="help-search" :placeholder="t('help.searchPlaceholder')" :input-props="{ 'aria-label': t('help.searchPlaceholder') }">
        <template #prefix><n-icon :component="SearchOutline" /></template>
      </n-input>
      <div class="chips">
        <n-button size="small" :type="category === '' ? 'primary' : 'default'" @click="category = ''">{{ t('help.all') }}</n-button>
        <n-button v-for="c in HELP_CATEGORIES" :key="c" size="small" :type="category === c ? 'primary' : 'default'" :data-testid="`help-cat-${c}`" @click="category = c">{{ categoryLabel(c) }}</n-button>
      </div>

      <p v-if="!shown.length" class="note" data-testid="help-empty">{{ t('help.empty') }}</p>
      <p v-else-if="query" class="note">{{ t('help.found', { n: shown.length }) }}</p>
      <ul class="list">
        <li v-for="a in shown" :key="a.slug">
          <router-link :to="{ name: 'help', params: { slug: a.slug } }" class="glass item rise" :data-testid="`help-item-${a.slug}`">
            <span class="tag">{{ categoryLabel(a.category) }}</span>
            <strong>{{ a.title }}</strong>
            <span class="note">{{ a.description }}</span>
          </router-link>
        </li>
      </ul>
      <p class="ask">
        {{ t('help.noAnswer') }}
        <router-link :to="{ name: 'support' }">{{ t('help.askSupport') }}</router-link>
      </p>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 16px;
  max-width: 860px;
}

.head h1,
.card h1 {
  font-size: clamp(24px, 3vw, 32px);
  font-weight: 800;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
}

.lead {
  font-size: 15px;
}

.card {
  padding: 22px 26px 26px;
  display: grid;
  gap: 14px;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}

.item {
  display: grid;
  gap: 4px;
  padding: 14px 18px;
  text-decoration: none;
  color: inherit;
  transition: transform 0.15s ease;
}

.item:hover {
  transform: translateY(-2px);
}

.tag {
  justify-self: start;
  padding: 1px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  background: rgba(139, 92, 246, 0.18);
  color: #c4b5fd;
}

.back {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 14px;
}

.ask {
  color: var(--text-dim);
  font-size: 14px;
}
</style>
