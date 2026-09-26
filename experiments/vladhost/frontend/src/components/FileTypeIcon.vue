<script setup lang="ts">
import {
  ArchiveOutline,
  CodeSlashOutline,
  DocumentOutline,
  DocumentTextOutline,
  FolderOutline,
  ImageOutline,
} from '@vicons/ionicons5'
import { NIcon } from 'naive-ui'
import { computed, type Component } from 'vue'

// Значок файла: папки, код, стили, изображения и т. д. — у каждого типа свой цвет.
const props = defineProps<{ name: string; dir?: boolean }>()

interface Look {
  icon: Component
  rgb: string // "r g b"
}

const byExt: Record<string, Look> = {
  html: { icon: CodeSlashOutline, rgb: '251 146 60' },
  htm: { icon: CodeSlashOutline, rgb: '251 146 60' },
  css: { icon: CodeSlashOutline, rgb: '96 165 250' },
  js: { icon: CodeSlashOutline, rgb: '250 204 21' },
  mjs: { icon: CodeSlashOutline, rgb: '250 204 21' },
  ts: { icon: CodeSlashOutline, rgb: '56 189 248' },
  json: { icon: DocumentTextOutline, rgb: '52 211 153' },
  md: { icon: DocumentTextOutline, rgb: '167 139 250' },
  txt: { icon: DocumentTextOutline, rgb: '148 163 184' },
  svg: { icon: ImageOutline, rgb: '244 114 182' },
  png: { icon: ImageOutline, rgb: '244 114 182' },
  jpg: { icon: ImageOutline, rgb: '244 114 182' },
  jpeg: { icon: ImageOutline, rgb: '244 114 182' },
  gif: { icon: ImageOutline, rgb: '244 114 182' },
  webp: { icon: ImageOutline, rgb: '244 114 182' },
  ico: { icon: ImageOutline, rgb: '244 114 182' },
  zip: { icon: ArchiveOutline, rgb: '251 191 36' },
}

const look = computed<Look>(() => {
  if (props.dir) return { icon: FolderOutline, rgb: '251 191 36' }
  const dot = props.name.lastIndexOf('.')
  const ext = dot < 0 ? '' : props.name.slice(dot + 1).toLowerCase()
  return byExt[ext] ?? { icon: DocumentOutline, rgb: '148 163 184' }
})
</script>

<template>
  <span class="tile" :style="{ '--c': look.rgb }">
    <n-icon :size="17" :component="look.icon" />
  </span>
</template>

<style scoped>
.tile {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: 9px;
  color: rgb(var(--c));
  background: rgb(var(--c) / 0.14);
  border: 1px solid rgb(var(--c) / 0.3);
}
</style>
