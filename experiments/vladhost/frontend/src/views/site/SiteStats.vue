<script setup lang="ts">
import { NAlert, NButton, NRadioButton, NRadioGroup } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { api, ApiError } from '@/api/client'
import { statsPeriods, statsSchema, type Site, type Stats, type StatsItem, type StatsPeriod } from '@/api/schemas'
import DayBars, { type Bar } from '@/components/DayBars.vue'
import EmptyState from '@/components/EmptyState.vue'
import { formatBytes, useI18n } from '@/i18n'
import { percent } from '@/lib/chart'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()

type Metric = 'visitors' | 'pages' | 'hits' | 'bytes'

const period = ref<StatsPeriod>(30)
const metric = ref<Metric>('visitors')
const view = ref<'chart' | 'table'>('chart')
const stats = ref<Stats | null>(null)
const loading = ref(true)
const error = ref('')

let seq = 0
async function load() {
  if (!store.logsAvailable) return
  const mine = ++seq
  error.value = ''
  try {
    const r = await api(`/api/sites/${props.site.id}/stats?days=${period.value}`, { schema: statsSchema })
    if (mine === seq) stats.value = r
  } catch (e) {
    if (mine === seq) error.value = e instanceof ApiError ? e.message : t('stats.loadFailed')
  } finally {
    if (mine === seq) loading.value = false
  }
}
watch(period, () => void load())
void load()

const num = (v: number) => new Intl.NumberFormat(locale.value).format(v)
const compact = (v: number) => new Intl.NumberFormat(locale.value, { notation: 'compact', maximumFractionDigits: 1 }).format(v)
const fmt = (m: Metric, v: number) => (m === 'bytes' ? formatBytes(v, locale.value) : num(v))
const axisFmt = (v: number) => (metric.value === 'bytes' ? formatBytes(v, locale.value) : compact(v))

const metricLabel = computed(() => ({
  visitors: t('stats.metric.visitors'),
  pages: t('stats.metric.pages'),
  hits: t('stats.metric.hits'),
  bytes: t('stats.metric.bytes'),
}))

// Подсказка каждого столбца перечисляет все показатели дня; выбранный идёт первым.
const bars = computed<Bar[]>(() => {
  const order: Metric[] = [metric.value, ...(['visitors', 'pages', 'hits', 'bytes'] as const).filter((m) => m !== metric.value)]
  return (stats.value?.days ?? []).map((d) => {
    const v: Record<Metric, number> = { visitors: d.visitors, pages: d.pages, hits: d.hits, bytes: d.bytes }
    return { day: d.date, value: v[metric.value], rows: order.map((m) => ({ label: metricLabel.value[m], value: fmt(m, v[m]) })) }
  })
})

const empty = computed(() => !!stats.value && stats.value.total.hits === 0)

const statusRows = computed(() => {
  const tot = stats.value?.total
  if (!tot) return []
  const all = tot.s2 + tot.s3 + tot.s4 + tot.s5
  return [
    { key: 's2', label: t('stats.status.s2'), n: tot.s2, color: '#0ca30c' },
    { key: 's3', label: t('stats.status.s3'), n: tot.s3, color: '#3987e5' },
    { key: 's4', label: t('stats.status.s4'), n: tot.s4, color: '#fab219' },
    { key: 's5', label: t('stats.status.s5'), n: tot.s5, color: '#d03b3b' },
  ].map((r) => ({ ...r, share: percent(r.n, all) }))
})

const itemLabel = (i: StatsItem) => (i.key === '…' ? t('stats.other') : i.key)
const maxCount = (items: StatsItem[]) => Math.max(1, ...items.map((i) => i.count))
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.stats') }}</h1>
      <p>{{ t('stats.hint') }}</p>
    </header>

    <n-alert v-if="!store.logsAvailable" type="info" :show-icon="false">{{ t('stats.unavailable') }}</n-alert>

    <template v-else>
      <section class="filters rise" style="--i: 1">
        <n-radio-group v-model:value="period" :aria-label="t('stats.periodLabel')">
          <n-radio-button v-for="d in statsPeriods" :key="d" :value="d">{{ t('stats.period', { n: d }) }}</n-radio-button>
        </n-radio-group>
      </section>

      <n-alert v-if="error" type="error" :show-icon="false">
        {{ error }} <n-button size="tiny" @click="load">{{ t('common.retry') }}</n-button>
      </n-alert>

      <div v-if="loading" class="skeleton" style="height: 300px" />

      <template v-else-if="stats">
        <section class="tiles rise" style="--i: 2">
          <article class="tile glass">
            <div class="label">{{ t('stats.metric.visitors') }}</div>
            <div class="value">{{ num(stats.total.visitors) }}</div>
            <div class="sub">{{ t('stats.visitorsSub') }}</div>
          </article>
          <article class="tile glass">
            <div class="label">{{ t('stats.metric.pages') }}</div>
            <div class="value">{{ num(stats.total.pages) }}</div>
            <div class="sub">{{ t('stats.pagesSub') }}</div>
          </article>
          <article class="tile glass">
            <div class="label">{{ t('stats.metric.hits') }}</div>
            <div class="value">{{ num(stats.total.hits) }}</div>
            <div class="sub">{{ t('stats.hitsSub', { n: num(stats.total.bots) }) }}</div>
          </article>
          <article class="tile glass">
            <div class="label">{{ t('stats.metric.bytes') }}</div>
            <div class="value">{{ formatBytes(stats.total.bytes, locale) }}</div>
            <div class="sub">{{ t('stats.bytesSub') }}</div>
          </article>
        </section>

        <empty-state v-if="empty" :title="t('stats.empty')" :hint="t('stats.emptyHint')" class="glass" />

        <template v-else>
          <section class="card glass rise" style="--i: 3">
            <div class="card-head">
              <h3>{{ t('stats.byDay') }}</h3>
              <n-radio-group v-if="view === 'chart'" v-model:value="metric" size="small" :aria-label="t('stats.metricLabel')">
                <n-radio-button value="visitors">{{ t('stats.metric.visitors') }}</n-radio-button>
                <n-radio-button value="pages">{{ t('stats.metric.pages') }}</n-radio-button>
                <n-radio-button value="hits">{{ t('stats.metric.hits') }}</n-radio-button>
                <n-radio-button value="bytes">{{ t('stats.metric.bytes') }}</n-radio-button>
              </n-radio-group>
              <span class="grow" />
              <n-button size="small" class="tint-violet" @click="view = view === 'chart' ? 'table' : 'chart'">
                {{ view === 'chart' ? t('stats.showTable') : t('stats.showChart') }}
              </n-button>
            </div>

            <day-bars v-if="view === 'chart'" :bars="bars" :format="axisFmt" :label="metricLabel[metric]" />

            <div v-else class="scroll">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('stats.cols.date') }}</th>
                    <th class="num">{{ t('stats.metric.visitors') }}</th>
                    <th class="num">{{ t('stats.metric.pages') }}</th>
                    <th class="num">{{ t('stats.metric.hits') }}</th>
                    <th class="num">{{ t('stats.cols.bots') }}</th>
                    <th class="num">{{ t('stats.metric.bytes') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="d in [...stats.days].reverse()" :key="d.date">
                    <td class="nowrap">{{ d.date }}</td>
                    <td class="num">{{ num(d.visitors) }}</td>
                    <td class="num">{{ num(d.pages) }}</td>
                    <td class="num">{{ num(d.hits) }}</td>
                    <td class="num">{{ num(d.bots) }}</td>
                    <td class="num nowrap">{{ formatBytes(d.bytes, locale) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <div class="two">
            <section class="card glass rise" style="--i: 4">
              <h3>{{ t('stats.topPages') }}</h3>
              <ul v-if="stats.top_pages.length" class="rank">
                <li v-for="i in stats.top_pages" :key="i.key">
                  <div class="rank-line">
                    <span class="rank-key">{{ itemLabel(i) }}</span>
                    <strong>{{ num(i.count) }}</strong>
                  </div>
                  <span class="rank-bar"><i :style="{ width: `${(i.count / maxCount(stats.top_pages)) * 100}%` }" /></span>
                </li>
              </ul>
              <p v-else class="muted">{{ t('stats.noPages') }}</p>
            </section>

            <section class="card glass rise" style="--i: 5">
              <h3>{{ t('stats.topRefs') }}</h3>
              <ul v-if="stats.top_refs.length" class="rank">
                <li v-for="i in stats.top_refs" :key="i.key">
                  <div class="rank-line">
                    <span class="rank-key">{{ itemLabel(i) }}</span>
                    <strong>{{ num(i.count) }}</strong>
                  </div>
                  <span class="rank-bar"><i :style="{ width: `${(i.count / maxCount(stats.top_refs)) * 100}%` }" /></span>
                </li>
              </ul>
              <p v-else class="muted">{{ t('stats.noRefs') }}</p>
            </section>
          </div>

          <section class="card glass rise" style="--i: 6">
            <h3>{{ t('stats.statusTitle') }}</h3>
            <div class="split" role="img" :aria-label="t('stats.statusTitle')">
              <i v-for="r in statusRows.filter((x) => x.n > 0)" :key="r.key" :style="{ flex: r.n, background: r.color }" />
            </div>
            <ul class="legend">
              <li v-for="r in statusRows" :key="r.key">
                <i class="swatch" :style="{ background: r.color }" />
                <span class="lg-label">{{ r.label }}</span>
                <strong>{{ num(r.n) }}</strong>
                <span class="lg-share">{{ r.share }}%</span>
              </li>
            </ul>
          </section>
        </template>

        <p class="note">{{ t('stats.note') }}</p>
      </template>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 1080px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.head p,
.note,
.muted {
  margin: 4px 0 0;
  color: var(--text-dim);
  font-size: 13.5px;
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 14px;
}

.tile {
  padding: 16px 18px;
}

.label {
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 650;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.value {
  font-size: 28px;
  font-weight: 800;
  letter-spacing: -0.03em;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
}

.sub {
  color: var(--text-dim);
  font-size: 13px;
}

.card {
  padding: 18px 22px 20px;
}

.card h3 {
  font-size: 16px;
  margin-bottom: 12px;
}

.card-head {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.card-head h3 {
  margin: 0;
}

.grow {
  flex: 1;
}

.two {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
  gap: 18px;
}

.rank {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}

.rank-line {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 13.5px;
}

.rank-key {
  min-width: 0;
  overflow-wrap: anywhere;
}

.rank-line strong {
  font-variant-numeric: tabular-nums;
}

.rank-bar {
  display: block;
  height: 6px;
  margin-top: 5px;
}

.rank-bar i {
  display: block;
  height: 100%;
  min-width: 2px;
  border-radius: 0 4px 4px 0; /* основание прямое, скруглён только конец столбца */
  background: #3987e5;
}

.split {
  display: flex;
  gap: 2px; /* зазор цвета поверхности между сегментами */
  height: 14px;
  margin-bottom: 14px;
}

.split i {
  display: block;
  min-width: 3px;
  border-radius: 3px;
}

.legend {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 8px;
}

.legend li {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13.5px;
}

.swatch {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 3px;
}

.lg-label {
  flex: 1;
  color: var(--text-dim);
}

.lg-share {
  min-width: 48px;
  text-align: right;
  color: var(--text-faint);
  font-variant-numeric: tabular-nums;
}

.scroll {
  overflow-x: auto;
  max-height: 420px;
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13.5px;
}

th {
  position: sticky;
  top: 0;
  padding: 8px 12px;
  text-align: left;
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  background: #141733;
  white-space: nowrap;
}

td {
  padding: 7px 12px;
  border-top: 1px solid var(--border);
  font-variant-numeric: tabular-nums;
}

.num {
  text-align: right;
}

.nowrap {
  white-space: nowrap;
}
</style>
