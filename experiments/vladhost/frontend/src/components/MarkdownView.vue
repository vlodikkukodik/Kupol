<script setup lang="ts">
import type { Block } from '@/lib/markdown'
import MdInline from './MdInline.vue'

defineProps<{ blocks: Block[] }>()
</script>

<template>
  <div class="md">
    <template v-for="(b, i) in blocks" :key="i">
      <h2 v-if="b.t === 'h2'"><md-inline :nodes="b.v" /></h2>
      <h3 v-else-if="b.t === 'h3'"><md-inline :nodes="b.v" /></h3>
      <p v-else-if="b.t === 'p'"><md-inline :nodes="b.v" /></p>
      <p v-else-if="b.t === 'note'" class="note"><md-inline :nodes="b.v" /></p>
      <ul v-else-if="b.t === 'ul'">
        <li v-for="(item, k) in b.items" :key="k"><md-inline :nodes="item" /></li>
      </ul>
      <ol v-else-if="b.t === 'ol'">
        <li v-for="(item, k) in b.items" :key="k"><md-inline :nodes="item" /></li>
      </ol>
      <pre v-else-if="b.t === 'code'"><code>{{ b.v }}</code></pre>
    </template>
  </div>
</template>

<style scoped>
.md {
  display: grid;
  gap: 12px;
  line-height: 1.6;
  font-size: 15px;
}

h2 {
  font-size: 20px;
  font-weight: 700;
  margin-top: 8px;
}

h3 {
  font-size: 16px;
  font-weight: 700;
}

ul,
ol {
  margin: 0;
  padding-left: 22px;
  display: grid;
  gap: 6px;
}

.note {
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid var(--border);
  border-left: 4px solid #8b5cf6;
  background: rgba(139, 92, 246, 0.1);
}

pre {
  margin: 0;
  padding: 12px 14px;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--border);
  overflow-x: auto;
  font-size: 13px;
}

code {
  font-size: 0.92em;
  word-break: break-word;
}

pre code {
  word-break: normal;
}
</style>
