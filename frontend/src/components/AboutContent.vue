<script setup lang="ts">
import { ABOUT_INTRO, ABOUT_SECTIONS, AUTHOR_TEXT, LEVEL_ROWS, PRIVACY_INTRO, PRIVACY_ITEMS } from '@/content/about'

// Неизменяемая часть страницы «О КУПОЛЕ»: справка, правила, политика конфиденциальности, об авторе. Не зависит ни от роутера, ни от
// хранилищ, ни от API — поэтому её можно отрендерить на сервере при сборке (пререндер) и отдать поисковику готовым текстом.
// Хронология и контакты живые и подгружаются в AboutView.
defineProps<{ contact?: string }>()
</script>

<template>
  <div class="about">
    <p class="about__intro" data-testid="about-intro">{{ ABOUT_INTRO }}</p>

    <nav class="about__toc" aria-label="Содержание страницы">
      <ul>
        <li v-for="s in ABOUT_SECTIONS" :key="s.id"><a :href="`#${s.id}`">{{ s.title }}</a></li>
        <li><a href="#levels">Уровни допуска</a></li>
        <li><a href="#timeline">Хронология</a></li>
        <li><a href="#privacy">Конфиденциальность</a></li>
        <li><a href="#author">Об авторе</a></li>
      </ul>
    </nav>

    <section v-for="s in ABOUT_SECTIONS" :id="s.id" :key="s.id" :aria-labelledby="`${s.id}-title`" class="about__section">
      <h2 :id="`${s.id}-title`">{{ s.title }}</h2>
      <p v-for="(p, i) in s.paragraphs" :key="i">{{ p }}</p>
      <ul v-if="s.items">
        <li v-for="(item, i) in s.items" :key="i">{{ item }}</li>
      </ul>
    </section>

    <section id="levels" aria-labelledby="levels-title" class="about__section">
      <h2 id="levels-title">Уровни допуска</h2>
      <p>Уровень определяет, какие документы, блоки и фрагменты вы видите. Он показан в вашем личном деле и на пропуске в шапке сайта.</p>
      <div class="about__table-wrap" role="region" aria-label="Уровни допуска" tabindex="0">
        <table class="about__table" data-testid="levels-table">
          <caption class="visually-hidden">Уровни допуска и как их получить</caption>
          <thead>
            <tr>
              <th scope="col">Уровень</th>
              <th scope="col">Звание</th>
              <th scope="col">Как получить</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in LEVEL_ROWS" :key="r.level">
              <th scope="row">{{ r.level }}</th>
              <td>{{ r.name }}</td>
              <td>{{ r.how }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- сюда AboutView вставляет хронологию -->
    <slot name="timeline" />

    <section id="privacy" aria-labelledby="privacy-title" class="about__section">
      <h2 id="privacy-title">Политика конфиденциальности</h2>
      <p>{{ PRIVACY_INTRO }}</p>
      <dl class="about__privacy" data-testid="privacy">
        <div v-for="item in PRIVACY_ITEMS" :key="item.term">
          <dt>{{ item.term }}</dt>
          <dd>{{ item.text }}</dd>
        </div>
      </dl>
    </section>

    <section id="author" aria-labelledby="author-title" class="about__section">
      <h2 id="author-title">Об авторе и контакты</h2>
      <p>{{ AUTHOR_TEXT }}</p>
      <p v-if="contact" class="about__contact" data-testid="author-contact">{{ contact }}</p>
    </section>
  </div>
</template>

<style scoped>
.about {
  max-width: 48rem;
}
.about__intro {
  margin-top: 0;
  font-size: var(--text-lg);
}
.about__toc ul {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  margin: var(--space-4) 0 var(--space-5);
  padding: 0;
  list-style: none;
}
.about__section {
  margin-top: var(--space-6);
  scroll-margin-top: var(--space-4);
}
.about__section h2 {
  margin: 0 0 var(--space-2);
  padding-bottom: var(--space-1);
  border-bottom: 2px solid var(--ink-900);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.about__table-wrap {
  overflow-x: auto;
}
.about__table {
  width: 100%;
  border-collapse: collapse;
}
.about__table th,
.about__table td {
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  text-align: left;
}
.about__table thead th {
  border-bottom: 2px solid var(--ink-900);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.about__privacy {
  margin: 0;
}
.about__privacy div {
  padding: var(--space-2) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.about__privacy dt {
  font-weight: 700;
}
.about__privacy dd {
  margin: var(--space-1) 0 0;
}
.about__contact {
  padding: var(--space-3);
  border-left: 4px solid var(--ink-900);
  background: var(--surface-sunken);
  overflow-wrap: anywhere;
}
</style>
