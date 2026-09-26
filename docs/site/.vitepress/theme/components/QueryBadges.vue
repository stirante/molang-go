<script setup lang="ts">
// A query page's badges, from the front matter tools/generate.mjs writes:
//
//   querySet      default | tags | world_gen
//   side          client | server | both, possibly with a note
//   deprecated    true when the query is deprecated
//   since, until  the Molang version range the name resolves in
//   clientOnly    true when only resource-pack Molang can use it
//   verification  { status, game, label }: how far the page is verified in game
import { useData } from 'vitepress'
import { computed } from 'vue'

const { frontmatter } = useData()
const fm = computed(() => frontmatter.value)
const side = computed(() => String(fm.value.side ?? '').split(' ')[0])
const v = computed(() => fm.value.verification as { status: string; game: string; label: string } | undefined)
const verifiedText: Record<string, string> = {
  verified: 'verified in game',
  partial: 'partly verified in game',
  documented: 'nothing to measure',
  unobservable: 'not observable in game',
  pending: 'in-game check pending',
}
</script>

<template>
  <div class="mg-badges" role="note">
    <span class="mg-badge mg-badge--set" title="Query set: which fields can name this query">{{ fm.querySet }}</span>
    <span class="mg-badge" :class="`mg-badge--side-${side}`" :title="`Where the value is meaningful: ${fm.side}`">{{ side }}</span>
    <span v-if="fm.clientOnly" class="mg-badge mg-badge--client" title="Only resource-pack (client) Molang can use it">client only</span>
    <span v-if="fm.deprecated" class="mg-badge mg-badge--deprecated" title="Deprecated: see the note below">deprecated</span>
    <span v-if="fm.until" class="mg-badge mg-badge--gate" :title="`Resolves only in files whose Molang version is below ${fm.until}`">before {{ fm.until }}</span>
    <span v-if="fm.since" class="mg-badge mg-badge--gate" :title="`Resolves only in files whose Molang version is ${fm.since} or later`">{{ fm.since }}+</span>
    <span v-if="v" class="mg-badge" :class="`mg-badge--v-${v.status}`" :title="v.label">{{ verifiedText[v.status] ?? v.status }} · {{ v.game }}</span>
  </div>
</template>
