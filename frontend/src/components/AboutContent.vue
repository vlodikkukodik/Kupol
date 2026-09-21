<script setup lang="ts">
import { computed } from 'vue'
import { aboutSections, levelRows, privacyItems } from '@/content/about'
import { linkifyContact } from '@/lib/contacts'

// computed: тексты пересчитываются при смене языка
const sections = computed(() => aboutSections())
const levels = computed(() => levelRows())
const privacy = computed(() => privacyItems())

// Неизменяемая часть страницы «О КУПОЛЕ»: справка, правила, политика конфиденциальности, об авторе. Не зависит ни от роутера, ни от
// хранилищ, ни от API — поэтому её можно отрендерить на сервере при сборке (пререндер) и отдать поисковику готовым текстом.
// Хронология и контакты живые и подгружаются в AboutView.
defineProps<{ contact?: string }>()
</script>

<template>
  <div class="about">
    <p class="about__intro" data-testid="about-intro">{{ $t('about.intro') }}</p>

    <nav class="about__toc" :aria-label="$t('about.toc')">
      <ul>
        <li v-for="s in sections" :key="s.id"><a :href="`#${s.id}`">{{ s.title }}</a></li>
        <li><a href="#levels">{{ $t('about.tocLevels') }}</a></li>
        <li><a href="#timeline">{{ $t('about.tocTimeline') }}</a></li>
        <li><a href="#privacy">{{ $t('about.tocPrivacy') }}</a></li>
        <li><a href="#author">{{ $t('about.tocAuthor') }}</a></li>
      </ul>
    </nav>

    <section v-for="s in sections" :id="s.id" :key="s.id" :aria-labelledby="`${s.id}-title`" class="about__section">
      <h2 :id="`${s.id}-title`">{{ s.title }}</h2>
      <p v-for="(p, i) in s.paragraphs" :key="i">{{ p }}</p>
      <ul v-if="s.items">
        <li v-for="(item, i) in s.items" :key="i">{{ item }}</li>
      </ul>
    </section>

    <section id="levels" aria-labelledby="levels-title" class="about__section">
      <h2 id="levels-title">{{ $t('about.levels.title') }}</h2>
      <p>{{ $t('about.levels.lead') }}</p>
      <div class="about__table-wrap" role="region" :aria-label="$t('about.levels.region')" tabindex="0">
        <table class="about__table" data-testid="levels-table">
          <caption class="visually-hidden">{{ $t('about.levels.caption') }}</caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('about.levels.colLevel') }}</th>
              <th scope="col">{{ $t('about.levels.colRank') }}</th>
              <th scope="col">{{ $t('about.levels.colHow') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in levels" :key="r.level">
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
      <h2 id="privacy-title">{{ $t('about.privacy.title') }}</h2>
      <p>{{ $t('about.privacy.intro') }}</p>
      <dl class="about__privacy" data-testid="privacy">
        <div v-for="item in privacy" :key="item.term">
          <dt>{{ item.term }}</dt>
          <dd>{{ item.text }}</dd>
        </div>
      </dl>
    </section>

    <section id="author" aria-labelledby="author-title" class="about__section">
      <h2 id="author-title">{{ $t('about.author.title') }}</h2>
      <p>{{ $t('about.author.text') }}</p>
      <div v-if="contact" class="about__contact" data-testid="author-contact">
        <p v-for="(line, i) in contact.split('\n')" :key="i" class="about__contact-line">
          <template v-for="(part, j) in linkifyContact(line)" :key="j">
            <a v-if="part.href" :href="part.href" rel="noopener noreferrer nofollow">{{ part.text }}</a><template v-else>{{ part.text }}</template>
          </template>
        </p>
      </div>
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
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--ink-900);
  background: var(--surface-sunken);
  overflow-wrap: anywhere;
}
.about__contact-line {
  margin: var(--space-1) 0;
  min-height: 1em;
}
</style>
