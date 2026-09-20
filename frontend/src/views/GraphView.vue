<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { isApiError } from '@/api/client'
import { documentsApi } from '@/api/endpoints'
import type { GraphNode } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { CARD_H, CARD_W, layoutBoard, pinOf, threadPath, wrapTitle } from '@/lib/graph'
import ErrorView from './ErrorView.vue'
import NotFoundView from './NotFoundView.vue'

// «Доска с нитками»: документ в центре, связанные с ним ссылками — вокруг, красные нити между карточками. Показано только то, что
// читатель вправе видеть (сервер отдаёт доску уже отфильтрованной). Карточка ведёт к доске вокруг неё; тот же граф списком — под доской.
const route = useRoute()
const router = useRouter()

const ref = computed(() => String(route.params.ref))
const depth = computed<1 | 2>(() => (route.query.depth === '2' ? 2 : 1))

const query = useQuery({
  queryKey: computed(() => keys.graph(ref.value, depth.value)),
  queryFn: ({ signal }) => documentsApi.graph(ref.value, depth.value, { signal }),
  retry: false,
  staleTime: 0,
})
const data = computed(() => query.data.value ?? null)
const error = computed(() => (isApiError(query.error.value) ? query.error.value : null))
const board = computed(() => (data.value ? layoutBoard(data.value.nodes, data.value.edges) : null))
const center = computed(() => data.value?.nodes.find((n) => n.depth === 0) ?? null)
const nodeName = (code: string) => data.value?.nodes.find((n) => n.code === code)

/** Небольшой наклон карточки — от шифра, а не случайный: доска не «дрожит» при перерисовке. */
function tilt(code: string): number {
  let h = 0
  for (const ch of code) h = (h * 31 + ch.charCodeAt(0)) % 997
  return ((h % 7) - 3) * 0.5 // −1.5°…+1.5°
}

const label = (n: GraphNode) => `${n.code} — ${n.title}, ${n.type_name}, ${n.year}` + (n.depth === 0 ? '. Центр доски' : '. Показать связи этого документа')

function href(n: GraphNode) {
  return router.resolve({ name: 'graph', params: { ref: n.slug }, query: route.query }).href
}
function recenter(n: GraphNode) {
  if (n.depth === 0) return
  void router.push({ name: 'graph', params: { ref: n.slug }, query: route.query })
}
function setDepth(d: 1 | 2) {
  const q = { ...route.query }
  if (d === 2) q.depth = '2'
  else delete q.depth
  void router.push({ query: q })
}
</script>

<template>
  <NotFoundView v-if="error && error.status === 404" />
  <ErrorView v-else-if="error" :request-id="error.requestId" :retrying="query.isFetching.value" @retry="query.refetch()" />

  <div v-else>
    <UiPageHeader title="Связи документа" kicker="Доска Центрального архива" />

    <UiSheet wide data-testid="graph">
      <UiSkeleton v-if="query.isPending.value" :lines="6" label="Собираем доску…" />

      <template v-else-if="data && board && center">
        <p class="lead">
          Документ
          <RouterLink :to="{ name: 'document', params: { ref: center.slug } }" data-testid="graph-center-link">{{ center.code }} — {{ center.title }}</RouterLink>
          и то, что с ним связано ссылками. Показано только доступное вам.
        </p>

        <div class="controls" role="group" aria-label="Глубина связей">
          <button type="button" class="depth" :aria-pressed="depth === 1 ? 'true' : 'false'" data-testid="depth-1" @click="setDepth(1)">Прямые связи</button>
          <button type="button" class="depth" :aria-pressed="depth === 2 ? 'true' : 'false'" data-testid="depth-2" @click="setDepth(2)">Через одного</button>
        </div>

        <p v-if="data.nodes.length === 1" class="empty" data-testid="graph-empty">У этого документа нет связей, доступных вам.</p>
        <p v-if="data.truncated" class="note" data-testid="graph-truncated">Связей больше, чем помещается на доске: показаны первые по шифру.</p>

        <div class="wrap" tabindex="0" role="region" aria-label="Доска связей; прокручивается по горизонтали">
          <svg
            class="board"
            :viewBox="`${board.view.x} ${board.view.y} ${board.view.w} ${board.view.h}`"
            :style="{ maxWidth: `${board.view.w}px`, minWidth: `${Math.min(board.view.w, 640)}px` }"
            role="group"
            aria-label="Карточки документов и нити между ними"
            data-testid="board"
          >
            <g class="threads" aria-hidden="true">
              <path v-for="e in data.edges" :key="`${e.from}>${e.to}`" :d="threadPath(board.byCode.get(e.from)!, board.byCode.get(e.to)!)" class="thread" :data-edge="`${e.from}>${e.to}`" />
            </g>
            <a
              v-for="p in board.placed"
              :key="p.node.code"
              class="card"
              :class="{ 'card--center': p.node.depth === 0 }"
              :href="href(p.node)"
              :aria-label="label(p.node)"
              :aria-current="p.node.depth === 0 ? 'true' : undefined"
              :data-code="p.node.code"
              @click.prevent="recenter(p.node)"
            >
              <g :transform="`translate(${p.x} ${p.y}) rotate(${tilt(p.node.code)})`">
                <rect class="card__paper" :x="-CARD_W / 2" :y="-CARD_H / 2" :width="CARD_W" :height="CARD_H" rx="2" />
                <text class="card__code" :x="-CARD_W / 2 + 10" :y="-CARD_H / 2 + 32">{{ p.node.code }}</text>
                <text v-for="(line, i) in wrapTitle(p.node.title, 24, 2)" :key="i" class="card__title" :x="-CARD_W / 2 + 10" :y="-CARD_H / 2 + 50 + i * 15">{{ line }}</text>
                <text class="card__meta" :x="-CARD_W / 2 + 10" :y="CARD_H / 2 - 6">{{ p.node.type_name }} · {{ p.node.year }}<tspan v-if="p.node.level > 0"> · допуск {{ p.node.level }}</tspan></text>
              </g>
              <circle class="pin" :cx="pinOf(p).x" :cy="pinOf(p).y" r="5" aria-hidden="true" />
            </a>
          </svg>
        </div>

        <h2 class="sub">Списком</h2>
        <div class="lists">
          <section aria-labelledby="graph-docs">
            <h3 id="graph-docs">Документы ({{ data.nodes.length }})</h3>
            <ul data-testid="graph-nodes">
              <li v-for="n in data.nodes" :key="n.code">
                <RouterLink :to="{ name: 'document', params: { ref: n.slug } }">{{ n.code }} — {{ n.title }}</RouterLink>
                <span class="muted"> · {{ n.type_name }}<template v-if="n.depth === 0"> · центр</template></span>
                <template v-if="n.depth !== 0">
                  · <RouterLink :to="{ name: 'graph', params: { ref: n.slug }, query: route.query }">связи</RouterLink>
                </template>
              </li>
            </ul>
          </section>
          <section aria-labelledby="graph-edges">
            <h3 id="graph-edges">Ссылки ({{ data.edges.length }})</h3>
            <ul v-if="data.edges.length" data-testid="graph-edges">
              <li v-for="e in data.edges" :key="`${e.from}>${e.to}`">
                {{ e.from }} <span aria-label="ссылается на">→</span> {{ e.to }}
                <span class="muted"> · {{ nodeName(e.from)?.title }} → {{ nodeName(e.to)?.title }}</span>
              </li>
            </ul>
            <p v-else class="muted">Ссылок между показанными документами нет.</p>
          </section>
        </div>
      </template>
    </UiSheet>
  </div>
</template>

<style scoped>
.lead {
  margin-top: 0;
  overflow-wrap: anywhere;
}
.controls {
  display: inline-flex;
  margin-bottom: var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  overflow: hidden;
}
.depth {
  min-height: var(--control-h);
  padding: 0 var(--space-5);
  border: 0;
  background: transparent;
  color: var(--ink-900);
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.depth + .depth {
  border-left: 2px solid var(--ink-900);
}
.depth[aria-pressed='true'] {
  background: var(--ink-900);
  color: var(--paper-100);
  font-weight: 700;
}
.empty,
.note {
  margin: 0 0 var(--space-3);
  color: var(--text-muted);
}
.wrap {
  overflow-x: auto;
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  /* пробковая доска */
  background: #c9b48c;
  background-image: radial-gradient(rgb(0 0 0 / 0.07) 1px, transparent 1px);
  background-size: 6px 6px;
}
.board {
  display: block;
  width: 100%;
  height: auto;
  margin: 0 auto;
}
.thread {
  fill: none;
  stroke: #a3261b;
  stroke-width: 2.4;
  stroke-linecap: round;
  filter: drop-shadow(0 2px 1px rgb(0 0 0 / 0.3));
}
.card {
  cursor: pointer;
  outline: none;
}
.card__paper {
  fill: #f5efdc;
  stroke: var(--ink-900);
  stroke-width: 1.2;
  filter: drop-shadow(0 3px 2px rgb(0 0 0 / 0.35));
}
.card--center .card__paper {
  fill: #fffaf0;
  stroke-width: 3;
}
.card:hover .card__paper {
  stroke-width: 2.4;
}
.card:focus-visible .card__paper {
  stroke: var(--focus);
  stroke-width: 4;
}
.card__code {
  fill: var(--ink-900);
  font-family: var(--font-doc);
  font-size: 15px;
  font-weight: 700;
}
.card__title {
  fill: var(--ink-900);
  font-family: var(--font-head);
  font-size: 13px;
}
.card__meta {
  fill: #4a4438;
  font-family: var(--font-head);
  font-size: 10.5px;
  letter-spacing: 0.04em;
}
.pin {
  fill: #a3261b;
  stroke: #5b0f09;
  stroke-width: 1;
}
.sub {
  margin: var(--space-6) 0 var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-md);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.lists {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
  gap: var(--space-5);
}
.lists h3 {
  margin: 0 0 var(--space-2);
  font-size: var(--text-sm);
  color: var(--text-muted);
}
.lists ul {
  margin: 0;
  padding-left: var(--space-5);
}
.lists li {
  overflow-wrap: anywhere;
}
.muted {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
