<script setup lang="ts">
import { computed, ref } from 'vue'
import { dayDate, labelIndexes, niceMax, ticks } from '@/lib/chart'
import { useI18n } from '@/i18n'

// Столбчатый график по суткам: одна серия, ось от нуля, подсказка со всеми показателями дня.
// Столбец — цель наведения целиком (с зазором), с клавиатуры график листается стрелками.
export interface Bar {
  day: string // YYYY-MM-DD
  value: number
  rows: { label: string; value: string }[] // строки подсказки: показатель и значение
}

const props = defineProps<{
  bars: Bar[]
  format: (v: number) => string // подпись значения на оси
  label: string // что показано (для скринридера)
}>()

const { locale } = useI18n()

const max = computed(() => niceMax(Math.max(0, ...props.bars.map((b) => b.value))))
const axis = computed(() => ticks(max.value))
const xLabels = computed(() => new Set(labelIndexes(props.bars.length, props.bars.length > 14 ? 6 : 7)))

const dayFmt = computed(() => new Intl.DateTimeFormat(locale.value, { day: 'numeric', month: 'short', timeZone: 'UTC' }))
const dayLong = computed(() => new Intl.DateTimeFormat(locale.value, { dateStyle: 'full', timeZone: 'UTC' }))
const short = (d: string) => dayFmt.value.format(dayDate(d))
const long = (d: string) => dayLong.value.format(dayDate(d))

const active = ref<number | null>(null)
const n = computed(() => props.bars.length)

function onKey(e: KeyboardEvent) {
  if (!n.value) return
  const last = n.value - 1
  const cur = active.value ?? last + 1
  if (e.key === 'ArrowLeft') active.value = Math.max(0, cur - 1)
  else if (e.key === 'ArrowRight') active.value = Math.min(last, cur + 1)
  else if (e.key === 'Home') active.value = 0
  else if (e.key === 'End') active.value = last
  else if (e.key === 'Escape') active.value = null
  else return
  e.preventDefault()
}

// Подсказка прижимается к краю графика, чтобы не вылезать за него.
const tipStyle = computed(() => {
  const i = active.value
  if (i === null || !n.value) return {}
  const center = ((i + 0.5) / n.value) * 100
  if (i < n.value * 0.18) return { left: `${center}%`, transform: 'translateX(-12px)' }
  if (i > n.value * 0.82) return { left: `${center}%`, transform: 'translateX(calc(-100% + 12px))' }
  return { left: `${center}%`, transform: 'translateX(-50%)' }
})
</script>

<template>
  <div class="chart" @pointerleave="active = null">
    <div class="yaxis" aria-hidden="true">
      <span v-for="(t, i) in [...axis].reverse()" :key="i">{{ format(t) }}</span>
    </div>
    <div
      class="plot"
      tabindex="0"
      role="img"
      :aria-label="`${label}: ${bars.length}`"
      @keydown="onKey"
      @blur="active = null"
    >
      <div class="grid" aria-hidden="true"><i /><i /><i /></div>
      <div class="cols">
        <div
          v-for="(b, i) in bars"
          :key="b.day"
          class="col"
          :class="{ on: active === i }"
          @pointerenter="active = i"
          @pointermove="active = i"
        >
          <span class="bar" :style="{ height: `${(b.value / max) * 100}%` }" />
        </div>
      </div>
      <div class="xaxis" aria-hidden="true">
        <span v-for="(b, i) in bars" :key="b.day" :class="{ show: xLabels.has(i) }">{{ xLabels.has(i) ? short(b.day) : '' }}</span>
      </div>
      <div v-if="active !== null && bars[active]" class="tip" :style="tipStyle" role="status">
        <div class="tip-day">{{ long(bars[active]!.day) }}</div>
        <div v-for="(r, i) in bars[active]!.rows" :key="i" class="tip-row" :class="{ lead: i === 0 }">
          <strong>{{ r.value }}</strong>
          <span>{{ r.label }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart {
  --bar: #3987e5; /* синий шаг тёмной палитры: контраст к тёмной поверхности выше 3:1 */
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 10px;
  height: 250px;
}

.yaxis {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  align-items: flex-end;
  padding-bottom: 26px; /* высота подписей оси X */
  color: var(--text-faint);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.plot {
  position: relative;
  min-width: 0;
  display: grid;
  grid-template-rows: minmax(0, 1fr) 26px;
  outline: none;
  border-radius: 8px;
}

.plot:focus-visible {
  box-shadow: 0 0 0 2px rgba(167, 139, 250, 0.7);
}

.grid {
  position: absolute;
  inset: 0 0 26px 0;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  pointer-events: none;
}

.grid i {
  display: block;
  height: 1px;
  background: rgba(255, 255, 255, 0.09);
}

.cols {
  position: relative;
  display: flex;
  min-height: 0;
}

.col {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 0 1px; /* вместе с соседним даёт зазор 2px между столбцами */
  cursor: default;
}

.bar {
  display: block;
  width: 100%;
  max-width: 24px;
  min-height: 0;
  background: var(--bar);
  border-radius: 4px 4px 0 0; /* скругление только у вершины, основание прямое */
  transition: filter 0.15s;
}

.col.on .bar {
  filter: brightness(1.25);
}

.col.on {
  background: rgba(255, 255, 255, 0.05);
}

.xaxis {
  display: flex;
  align-items: flex-end;
  color: var(--text-faint);
  font-size: 12px;
  white-space: nowrap;
}

.xaxis span {
  flex: 1;
  min-width: 0;
  display: flex;
  justify-content: center; /* подпись шире столбца расходится в обе стороны от его центра */
}

.tip {
  position: absolute;
  top: 6px;
  z-index: 5;
  min-width: 190px;
  padding: 10px 12px;
  border-radius: 12px;
  background: #171a35;
  border: 1px solid var(--border);
  box-shadow: 0 14px 34px -12px rgba(0, 0, 0, 0.7);
  pointer-events: none;
}

.tip-day {
  margin-bottom: 6px;
  color: var(--text-dim);
  font-size: 12px;
}

.tip-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
  color: var(--text-dim);
}

.tip-row strong {
  min-width: 56px;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.tip-row.lead strong {
  font-size: 15px;
}
</style>
