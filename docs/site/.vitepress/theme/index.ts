// The default theme plus two components: the badges at the top of each query
// page (from its front matter) and the searchable query index.
import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import QueryBadges from './components/QueryBadges.vue'
import QueryIndex from './components/QueryIndex.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('QueryBadges', QueryBadges)
    app.component('QueryIndex', QueryIndex)
  },
} satisfies Theme
