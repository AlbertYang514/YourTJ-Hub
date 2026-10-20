import { h } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import BadgesManagementPage from '../../../src/admin/pages/management/BadgesManagementPage.vue'
import { i18n, setLocale } from '../../../src/runtime/i18n'
import '../../../src/admin/styles/admin.css'
import { createBrowserApp } from './create-app'
const params = new URLSearchParams(location.search)
await setLocale((params.get('lang') || 'zh') as 'zh' | 'en' | 'de' | 'ja')
if (params.get('theme') === 'dark') { document.documentElement.classList.add('dark'); document.documentElement.dataset.theme = 'gf-dark' }
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: BadgesManagementPage }] })
createBrowserApp({ render: () => h(BadgesManagementPage, { payload: {} as never }) }).use(i18n).use(router).mount('#app')
