<script setup lang="ts">
import { computed } from 'vue'
import { asDocBlocks } from '@/api/blocks'
import type { OutDocument } from '@/api/generated/documents'
import BlockRenderer from '@/components/document/BlockRenderer.vue'
import { provideDocument } from '@/components/document/context'
import { archiveMark, archiveMarkText, classification, copyText } from '@/lib/realism'
import UiSheet from '@/ui/UiSheet.vue'
import UiStamp from '@/ui/UiStamp.vue'

// Лист документа так, как его видит читатель: шапка, блоки, подпись. Один компонент и для чтения, и для предпросмотра
// в панели команды — чтобы они не могли выглядеть по-разному.
const props = defineProps<{ doc: OutDocument }>()
const docRef = computed(() => props.doc)
provideDocument(docRef) // шапка досье берёт свойства документа отсюда
const blocks = computed(() => asDocBlocks(props.doc.blocks))
const mark = computed(() => archiveMark(props.doc))
</script>

<template>
  <UiSheet as="article" class="paper" :data-code="doc.code" fold>
    <header class="paper__head">
      <div class="paper__strip" data-testid="strip-top">
        <span>{{ doc.grif }}</span>
        <span class="paper__class" data-testid="classification">{{ classification(doc.level) }}</span>
        <span>{{ doc.code }}</span>
      </div>
      <p class="paper__kicker">{{ doc.type_name }} · {{ doc.code }}</p>
      <p class="paper__mark" data-testid="archive-mark">{{ archiveMarkText(mark) }} · {{ copyText(doc.copy_number) }}</p>
      <h1 data-doc-title tabindex="-1">{{ doc.title }}</h1>
      <UiStamp v-if="doc.status && doc.status !== 'published'" class="paper__status" :text="doc.status === 'draft' ? 'Черновик' : doc.status === 'review' ? 'На проверке' : 'Архив'" tone="ink" size="sm" />
    </header>

    <div class="paper__body">
      <!-- id блока — якорь для ссылок из поиска (/doc/О-041#b-intro); у закрытых блоков id нет -->
      <BlockRenderer v-for="(block, i) in blocks" :id="block.id ? `b-${block.id}` : undefined" :key="block.id ?? `redacted-${i}`" :block="block" />
    </div>

    <p v-if="doc.slug" class="paper__graph"><RouterLink :to="{ name: 'graph', params: { ref: doc.slug } }" data-testid="graph-link">Связи документа: доска с нитками →</RouterLink></p>

    <!-- Документы, которые ссылаются на этот: сервер отдаёт только доступные читателю (и документ, и сам блок-ссылку) -->
    <section v-if="doc.mentioned_in?.length" class="paper__mentions" aria-labelledby="mentioned-title" data-testid="mentions">
      <h2 id="mentioned-title">Упоминается в</h2>
      <ul>
        <li v-for="m in doc.mentioned_in" :key="m.code">
          <RouterLink :to="{ name: 'document', params: { ref: m.slug } }">{{ m.code }} — {{ m.title }}</RouterLink>
          <span class="paper__mention-type">{{ m.type_name }}</span>
        </li>
      </ul>
    </section>

    <!-- Лист ознакомления: сколько читателей открыло документ — без имён, история чтения закрыта -->
    <section v-if="doc.read_count > 0" class="paper__sheet" aria-labelledby="acquaint-title" data-testid="acquaint">
      <h2 id="acquaint-title">Лист ознакомления</h2>
      <p>Ознакомлено читателей: <strong>{{ doc.read_count }}</strong></p>
    </section>

    <footer v-if="doc.author" class="paper__foot">Составил(а): <strong>{{ doc.author }}</strong></footer>
    <div class="paper__strip paper__strip--bottom" data-testid="strip-bottom">
      <span>{{ classification(doc.level) }}</span>
      <span>{{ copyText(doc.copy_number) }}</span>
      <span>{{ doc.grif }}</span>
    </div>
  </UiSheet>
</template>

<style scoped>
.paper {
  margin-top: var(--space-4);
  padding: var(--space-6) var(--space-7);
  font-family: var(--font-doc);
  font-size: var(--text-md);
  line-height: 1.65;
}
.paper__head {
  position: relative;
  margin-bottom: var(--space-5);
}
.paper__strip {
  display: flex;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
  padding-bottom: var(--space-1);
  border-bottom: 3px double var(--ink-900);
  color: var(--text-muted);
  font-size: var(--text-xs);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
/* гриф секретности по центру колонтитула; нижний колонтитул повторяет его вместе с номером экземпляра */
.paper__class {
  flex: 1;
  color: var(--red-800);
  font-weight: 700;
  text-align: center;
}
.paper__strip--bottom {
  margin: var(--space-6) 0 0;
  padding: var(--space-1) 0 0;
  border-top: 3px double var(--ink-900);
  border-bottom: 0;
}
.paper__strip--bottom span:first-child {
  color: var(--red-800);
  font-weight: 700;
}
/* архивный шифр «Фонд · Опись · Дело · Листов» и экземпляр — служебная строка под видом документа */
.paper__mark {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.paper__sheet {
  margin-top: var(--space-5);
  padding: var(--space-2) var(--space-3);
  border: 1px dashed var(--border-strong);
}
.paper__sheet h2 {
  margin: 0;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.paper__sheet p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-sm);
}
.paper__kicker {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-family: var(--font-head);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.paper h1 {
  margin-bottom: 0;
  overflow-wrap: anywhere;
}
.paper h1:focus {
  outline: none;
}
.paper__status {
  position: absolute;
  top: var(--space-5);
  right: 0;
}
/* Блок, к которому привела ссылка из поиска: рамка на бумаге, а не только фокус (на touch-экранах фокус не виден) */
.paper__body :deep([data-found]) {
  outline: 3px solid var(--focus);
  outline-offset: 4px;
  background: rgb(255 226 122 / 0.35);
}
.paper__graph {
  margin: var(--space-6) 0 0;
  font-size: var(--text-sm);
}
.paper__mentions {
  margin-top: var(--space-4);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
}
.paper__mentions h2 {
  margin: 0 0 var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.paper__mentions ul {
  margin: 0;
  padding-left: var(--space-5);
}
.paper__mentions li {
  overflow-wrap: anywhere;
}
.paper__mention-type {
  margin-left: var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.paper__foot {
  margin-top: var(--space-7);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
@media (max-width: 48rem) {
  .paper {
    padding: var(--space-5) var(--space-4);
  }
  .paper__status {
    position: static;
    margin-top: var(--space-3);
  }
}
</style>
