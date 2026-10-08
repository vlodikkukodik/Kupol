<script setup lang="ts">
import { computed, ref } from 'vue'
import type { OutImage } from '@/api/generated/documents'

const props = defineProps<{ data: OutImage }>()
// Превью в тексте, оригинал (уже WebP, до 2400 px) — по щелчку; закрытый файл сервер отдаёт как «не найден»
const failed = ref(false)
const thumb = computed(() => `/api/uploads/${props.data.upload}/thumb`)
const full = computed(() => `/api/uploads/${props.data.upload}/file`)
</script>

<template>
  <figure class="image" :class="`image--${data.sticker}`">
    <p v-if="failed" class="image__missing" role="note">{{ $t('doc.imageMissing') }}</p>
    <a v-else :href="full" target="_blank" rel="noopener" class="image__link">
      <img :src="thumb" :alt="data.caption ?? ''" loading="lazy" @error="failed = true">
    </a>
    <figcaption v-if="data.caption">{{ data.caption }}</figcaption>
  </figure>
</template>

<style scoped>
.image {
  margin: var(--space-5) auto;
  max-width: 100%;
  width: fit-content;
  position: relative;
}
.image img {
  display: block;
  max-width: 100%;
  height: auto;
}
.image figcaption {
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-sm);
  text-align: center;
}
.image__missing {
  padding: var(--space-3);
  border: 1px dashed var(--text-muted);
  color: var(--text-muted);
}
.image--frame img {
  padding: var(--space-2);
  background: #fff;
  border: 1px solid var(--border-strong);
  box-shadow: 0 2px 6px rgb(0 0 0 / 0.25);
}
.image--clip::before {
  content: '';
  position: absolute;
  top: -14px;
  left: 12%;
  width: 14px;
  height: 44px;
  border: 3px solid #8b8b8b;
  border-radius: 8px;
  z-index: 1;
}
.image--stamp::after {
  content: '';
  position: absolute;
  right: -8px;
  bottom: 24px;
  width: 72px;
  height: 72px;
  border: 4px double var(--red-700);
  border-radius: 50%;
  opacity: 0.7;
  transform: rotate(-14deg);
  pointer-events: none;
}
</style>
