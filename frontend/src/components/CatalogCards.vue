<script setup lang="ts">
import type { Item } from '@/api/generated/documents'
import { statusNameLower } from '@/lib/catalog'

// «Картотека»: третий вид каталога — карточки, как в ящике архивной картотеки. Те же документы и порядок, что в реестре.
defineProps<{
  items: Item[]
  /** Директорат видит статус документа */
  showStatus?: boolean
}>()
</script>

<template>
  <ul class="cards" data-testid="cards">
    <li v-for="it in items" :key="it.slug" class="card" :data-code="it.code">
      <p class="card__strip">
        <span class="card__code">{{ it.code }}</span>
        <span>{{ it.type_name }}</span>
      </p>
      <h3 class="card__title">
        <RouterLink :to="{ name: 'document', params: { ref: it.slug } }">{{ it.title }}</RouterLink>
      </h3>
      <dl class="card__facts">
        <div>
          <dt>{{ $t('catalog.card.year') }}</dt>
          <dd>{{ it.composed.year }}</dd>
        </div>
        <div v-if="it.danger_class">
          <dt>{{ $t('catalog.card.class') }}</dt>
          <dd>{{ it.danger_class }}</dd>
        </div>
        <div v-if="it.department">
          <dt>{{ $t('catalog.card.department') }}</dt>
          <dd>{{ it.department }}</dd>
        </div>
        <div v-if="it.level > 0">
          <dt>{{ $t('catalog.card.access') }}</dt>
          <dd>{{ it.level }}</dd>
        </div>
        <div v-if="it.containment_name">
          <dt>{{ $t('catalog.card.containment') }}</dt>
          <dd>{{ it.containment_name }}</dd>
        </div>
      </dl>
      <p v-if="showStatus && it.status && it.status !== 'published'" class="card__status">{{ statusNameLower(it.status) }}</p>
    </li>
  </ul>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
  gap: var(--space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}
.card {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4) var(--space-4);
  border: 1px solid var(--border-strong);
  border-top: 8px solid var(--ink-900); /* корешок карточки */
  border-radius: 2px;
  background: var(--surface-raised);
  box-shadow: 0 2px 0 rgb(0 0 0 / 0.12);
}
.card__strip {
  display: flex;
  justify-content: space-between;
  gap: var(--space-2);
  margin: 0;
  padding-bottom: var(--space-1);
  border-bottom: 1px dashed var(--border-strong);
  color: var(--text-muted);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.card__code {
  font-family: var(--font-doc);
  font-weight: 700;
  color: var(--ink-900);
}
.card__title {
  margin: 0;
  font-family: var(--font-head);
  font-size: var(--text-lg);
  line-height: 1.25;
  overflow-wrap: anywhere;
}
.card__facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
  margin: auto 0 0;
  font-size: var(--text-sm);
}
.card__facts div {
  display: flex;
  gap: var(--space-1);
}
.card__facts dt {
  color: var(--text-muted);
}
.card__facts dt::after {
  content: ':';
}
.card__facts dd {
  margin: 0;
  font-weight: 700;
}
.card__status {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
