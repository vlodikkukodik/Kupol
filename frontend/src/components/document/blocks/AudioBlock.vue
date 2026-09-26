<script setup lang="ts">
import { computed } from 'vue'
import type { OutAudio } from '@/api/generated/documents'
import RichRuns from '../RichRuns.vue'

const props = defineProps<{ data: OutAudio }>()
const src = computed(() => `/api/uploads/${props.data.upload}/file`)
</script>

<template>
  <figure class="audio">
    <figcaption v-if="data.title">{{ data.title }}</figcaption>
    <audio controls preload="none" :src="src">{{ $t('doc.audioUnsupported') }}</audio>
    <details v-if="data.transcript?.length" class="audio__transcript">
      <summary>{{ $t('doc.transcript') }}</summary>
      <p><RichRuns :runs="data.transcript" /></p>
    </details>
  </figure>
</template>

<style scoped>
.audio {
  margin: var(--space-5) 0;
}
.audio figcaption {
  margin-bottom: var(--space-1);
  font-weight: 600;
}
.audio audio {
  width: 100%;
  max-width: 32rem;
}
.audio__transcript {
  margin-top: var(--space-2);
  color: var(--text-muted);
}
.audio__transcript p {
  white-space: pre-wrap;
}
</style>
