<script setup>
defineProps({
  types: { type: Array, required: true }, // [{type, name, count}] из /documents/summary
})
defineEmits(['open'])
</script>

<template>
  <ul class="folders" data-testid="folders">
    <li v-for="t in types" :key="t.type">
      <button type="button" class="folder" :data-type="t.type" @click="$emit('open', t.type)">
        <span class="tab" aria-hidden="true" />
        <span class="name">{{ t.name }}</span>
        <span class="count">{{ t.count }}</span>
      </button>
    </li>
  </ul>
</template>

<style scoped>
.folders {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(13rem, 1fr));
  gap: var(--space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}
.folder {
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  width: 100%;
  min-height: 7rem;
  padding: var(--space-3) var(--space-3) var(--space-2);
  border: 2px solid var(--ink);
  background: #e0cf9c;
  color: var(--ink);
  text-align: left;
  cursor: pointer;
}
.folder:hover { background: #d6c286; }
.tab {
  position: absolute;
  top: -0.7rem;
  left: 0.8rem;
  width: 4.5rem;
  height: 0.7rem;
  border: 2px solid var(--ink);
  border-bottom: 0;
  background: inherit;
}
.name { font-family: var(--font-head); font-size: 1.1rem; letter-spacing: 0.08em; text-transform: uppercase; }
.count { align-self: flex-end; font-size: 1.6rem; font-weight: 700; }
</style>
