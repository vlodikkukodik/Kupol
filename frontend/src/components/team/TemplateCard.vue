<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { TemplateItem } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { useDocumentMeta } from '@/composables/useDocumentMeta'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { blockPreview } from '@/lib/teamdoc'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import { useAuthStore } from '@/stores/auth'

// Карточка шаблона: название и описание, состав (блоки), «Создать документ» (шаблон документа), правка и удаление (право
// manage_templates — его показывает сервер в can_edit). Название и описание правятся здесь; состав — пересохранением из документа.
const props = defineProps<{ item: TemplateItem }>()
const emit = defineEmits<{ changed: [message: string] }>()

const auth = useAuthStore()
const { blockKindName } = useDocumentMeta()
const uid = useId()

const expanded = ref(false)
const detail = useQuery({
  queryKey: computed(() => keys.teamTemplate(props.item.id)),
  queryFn: ({ signal }) => teamApi.template(props.item.id, { signal }),
  enabled: expanded,
  staleTime: 0,
})

// ——— правка названия и описания ———
const editing = ref(false)
const name = ref('')
const description = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

function startEdit() {
  name.value = props.item.name
  description.value = props.item.description
  errors.value = {}
  failure.value = ''
  editing.value = true
}

function apiFail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  errors.value = { ...err.fields }
  failure.value = Object.keys(err.fields).length ? '' : describeApiError(err)
}

async function saveEdit() {
  errors.value = {}
  failure.value = ''
  if (!name.value.trim()) {
    errors.value = { name: 'Введите название' }
    return
  }
  busy.value = true
  try {
    await teamApi.updateTemplate(props.item.id, { name: name.value, description: description.value })
    editing.value = false
    emit('changed', `Шаблон «${name.value.trim()}» сохранён.`)
  } catch (err) {
    apiFail(err)
  } finally {
    busy.value = false
  }
}

// ——— удаление ———
const removing = ref(false)
async function confirmRemove() {
  failure.value = ''
  busy.value = true
  try {
    await teamApi.deleteTemplate(props.item.id)
    removing.value = false
    emit('changed', `Шаблон «${props.item.name}» удалён. Документы, созданные по нему, не изменились.`)
  } catch (err) {
    apiFail(err)
  } finally {
    busy.value = false
  }
}

const isDocument = computed(() => props.item.kind === 'document')
const blocksWord = (n: number) => (n % 10 === 1 && n % 100 !== 11 ? 'блок' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 12 || n % 100 > 14) ? 'блока' : 'блоков')
</script>

<template>
  <article class="tpl" :data-template="item.id" :aria-labelledby="`tpl-${uid}`">
    <div class="tpl__head">
      <h3 :id="`tpl-${uid}`" class="tpl__name">{{ item.name }}</h3>
      <p class="tpl__meta">
        <template v-if="isDocument">{{ item.doc_type_name }} · </template>{{ item.blocks }} {{ blocksWord(item.blocks) }} · {{ item.author ? `автор ${item.author}` : 'автор не указан' }} · обновлён {{ formatDateTime(item.updated_at) }}
      </p>
    </div>
    <p v-if="item.description" class="tpl__desc">{{ item.description }}</p>

    <div class="tpl__actions">
      <UiButton
        v-if="isDocument && auth.can('write_drafts')"
        :to="{ name: 'team-document-new', query: { template: String(item.id) } }"
        variant="primary"
        icon="plus"
        data-testid="tpl-use"
      >
        Создать документ
      </UiButton>
      <UiButton size="sm" :aria-expanded="expanded ? 'true' : 'false'" data-testid="tpl-toggle" @click="expanded = !expanded">{{ expanded ? 'Скрыть состав' : 'Показать состав' }}</UiButton>
      <template v-if="item.can_edit">
        <UiButton size="sm" icon="edit" data-testid="tpl-edit" @click="startEdit">Переименовать</UiButton>
        <UiButton size="sm" variant="ghost" icon="trash" data-testid="tpl-delete" @click="removing = true">Удалить</UiButton>
      </template>
    </div>
    <p v-if="!isDocument" class="tpl__hint">Набор блоков вставляется в документ в редакторе: «Вставить набор блоков» на панели инструментов.</p>

    <div v-if="expanded" class="tpl__detail" data-testid="tpl-detail">
      <UiSkeleton v-if="detail.isPending.value" :lines="3" label="Загружаем состав…" />
      <UiAlert v-else-if="detail.isError.value" tone="danger">Не удалось загрузить состав шаблона.</UiAlert>
      <template v-else-if="detail.data.value">
        <dl v-if="isDocument" class="facts">
          <div v-if="detail.data.value.content.title"><dt>Название</dt><dd>{{ detail.data.value.content.title }}</dd></div>
          <div v-if="detail.data.value.content.level !== undefined"><dt>Допуск</dt><dd>{{ detail.data.value.content.level }}</dd></div>
          <div v-if="detail.data.value.content.grif"><dt>Гриф</dt><dd>{{ detail.data.value.content.grif }}</dd></div>
        </dl>
        <ol class="blocks">
          <li v-for="(b, i) in detail.data.value.content.blocks" :key="b.id || i">
            <strong>{{ blockKindName(b.type) }}</strong>
            <span v-if="b.level" class="lvl">допуск {{ b.level }}</span>
            <span class="pv">{{ blockPreview(b, 90) }}</span>
          </li>
        </ol>
      </template>
    </div>

    <UiModal v-model:open="editing" title="Название и описание шаблона" testid="tpl-edit-dialog">
      <form novalidate @submit.prevent="saveEdit">
        <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
        <UiField label="Название" required :error="errors.name">
          <UiInput v-model="name" :maxlength="100" name="name" />
        </UiField>
        <UiField label="Описание (необязательно)" :error="errors.description">
          <UiTextarea v-model="description" :rows="3" :maxlength="500" name="description" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="tpl-save">Сохранить</UiButton>
          <UiButton variant="link" @click="editing = false">Отмена</UiButton>
        </div>
      </form>
    </UiModal>

    <UiModal v-model:open="removing" title="Удалить шаблон?" testid="tpl-delete-dialog">
      <p>Шаблон «{{ item.name }}» будет удалён навсегда. Документы, созданные по нему, и уже вставленные блоки не изменятся.</p>
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <div class="dlg-actions">
        <UiButton variant="danger" icon="trash" :loading="busy" data-testid="tpl-confirm-delete" @click="confirmRemove">Удалить</UiButton>
        <UiButton variant="link" @click="removing = false">Отмена</UiButton>
      </div>
    </UiModal>
  </article>
</template>

<style scoped>
.tpl {
  padding: var(--space-4);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
}
.tpl__name {
  margin: 0;
  overflow-wrap: anywhere;
}
.tpl__meta,
.tpl__hint {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.tpl__desc {
  margin: var(--space-2) 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.tpl__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-3);
}
.tpl__detail {
  margin-top: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px dashed var(--border-strong);
}
.facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-5);
  margin: 0 0 var(--space-3);
}
.facts dt {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.facts dd {
  margin: 0;
  font-weight: 700;
}
.blocks {
  margin: 0;
  padding-left: var(--space-5);
}
.blocks li {
  padding: var(--space-1) 0;
}
.lvl {
  margin-left: var(--space-2);
  color: var(--red-700);
  font-size: var(--text-sm);
}
.pv {
  display: block;
  color: var(--text-muted);
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
.dlg-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}
</style>
