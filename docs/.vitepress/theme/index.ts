// https://vitepress.dev/guide/custom-theme
import { h } from 'vue'
import { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import VPFeatures from './components/VPFeatures.vue'
import './style.css'

export default {
  extends: DefaultTheme,
  Layout() {
    return h(DefaultTheme.Layout, null, {})
  },
  enhanceApp({ app }) {
    app.component('VPFeatures', VPFeatures)
  },
} satisfies Theme
