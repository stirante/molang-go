<script setup lang="ts">
// The query index: every query, grouped by query set, filtered as you type.
// The list is generated/query-index.json, written by tools/generate.mjs.
import { withBase } from 'vitepress'
import { computed, ref } from 'vue'
import index from '../../../generated/query-index.json'

type Entry = (typeof index.queries)[number]
const text = ref('')
const side = ref('')
const flags = ref({ deprecated: false, gated: false, clientOnly: false, hideDeprecated: false })

const matches = (q: Entry) => {
  const t = text.value.trim().toLowerCase().replace(/^(query|q)\./, '')
  if (t && !q.name.includes(t) && !q.summary.toLowerCase().includes(t)) return false
  if (side.value && q.side !== side.value) return false
  if (flags.value.deprecated && !q.deprecated) return false
  if (flags.value.hideDeprecated && q.deprecated) return false
  if (flags.value.gated && !q.gate) return false
  if (flags.value.clientOnly && !q.clientOnly) return false
  return true
}
const groups = computed(() =>
  Object.entries(index.sets as Record<string, string>)
    .map(([set, title]) => ({ set, title, items: index.queries.filter((q) => q.set === set && matches(q)) }))
    .filter((g) => g.items.length),
)
const shown = computed(() => groups.value.reduce((n, g) => n + g.items.length, 0))
const vText: Record<string, string> = { verified: 'verified in game', partial: 'partly verified', documented: 'nothing to measure', unobservable: 'not observable in game', pending: 'check pending' }
</script>

<template>
  <div class="mg-index">
    <div class="mg-index__controls">
      <input v-model="text" class="mg-index__search" type="search" placeholder="Filter by name or summary (e.g. item, rotation)" aria-label="Filter queries" />
      <div class="mg-index__filters">
        <select v-model="side" aria-label="Side">
          <option value="">any side</option>
          <option value="client">client</option>
          <option value="server">server</option>
          <option value="both">both</option>
        </select>
        <label><input v-model="flags.hideDeprecated" type="checkbox" /> hide deprecated</label>
        <label><input v-model="flags.deprecated" type="checkbox" /> only deprecated</label>
        <label><input v-model="flags.gated" type="checkbox" /> only version-gated</label>
        <label><input v-model="flags.clientOnly" type="checkbox" /> only client-only</label>
      </div>
      <p class="mg-index__count">{{ shown }} of {{ index.queries.length }} queries</p>
    </div>
    <section v-for="g in groups" :key="g.set">
      <h2 :id="`set-${g.set}`">{{ g.title }} <span class="mg-index__set">{{ g.set }}</span></h2>
      <ul class="mg-index__list">
        <li v-for="q in g.items" :key="q.name">
          <a :href="withBase(`/queries/${q.name}`)"><code>query.{{ q.name }}</code></a>
          <span class="mg-index__badges">
            <span v-if="q.deprecated" class="mg-badge mg-badge--deprecated">deprecated</span>
            <span v-if="q.gate?.until" class="mg-badge mg-badge--gate">before {{ q.gate.until }}</span>
            <span v-if="q.gate?.since" class="mg-badge mg-badge--gate">{{ q.gate.since }}+</span>
            <span v-if="q.clientOnly" class="mg-badge mg-badge--client">client only</span>
            <span class="mg-badge" :class="`mg-badge--side-${q.side}`">{{ q.side }}</span>
            <span class="mg-badge" :class="`mg-badge--v-${q.verification}`">{{ vText[q.verification] }}</span>
          </span>
          <div class="mg-index__summary">{{ q.summary }}</div>
        </li>
      </ul>
    </section>
    <p v-if="!shown">No query matches.</p>
  </div>
</template>
