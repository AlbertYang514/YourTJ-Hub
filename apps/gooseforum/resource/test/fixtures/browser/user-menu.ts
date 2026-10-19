import { h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { LayoutPayload } from '@gooseforum/client'
import { i18n, setLocale } from '../../../src/runtime/i18n'
import AppShell from '../../../src/site/components/AppShell.vue'
import { createBrowserApp } from './create-app'
import '../../../src/styles/resource.css'

const params = new URLSearchParams(location.search)
document.documentElement.dataset.theme = params.get('theme') === 'dark' ? 'gf-dark' : 'gf-light'
await setLocale('zh')

const layout = {
  site: { name: 'yourtj', description: '', logo: '', favicon: '', brandType: 'text', brandText: 'yourtj', brandImage: '' },
  viewer: {
    id: 42,
    username: 'menu-tester',
    email: '',
    avatarUrl: '/assets/test/fixtures/browser/anonymous-admin-avatar-1.svg',
    isAuthenticated: true,
    canAccessAdmin: true,
    canManageAgents: true,
    isModerator: false,
    requiresEmailVerification: false,
    adminPermissions: [],
  },
  sidebar: { main: [], resources: [], groups: [], categories: [], activeKey: 'topics' },
  footer: { links: [], primary: [] },
  unread: { notifications: false, messages: false, moderationReports: false },
  theme: { enabled: false, current: 'gf-light', themeColor: '#fbfdff' },
  umamiEnabled: false,
} as LayoutPayload

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }],
})
await router.push('/')
await router.isReady()

createBrowserApp({
  render: () => h(AppShell, { layout }, { default: () => h('main', { class: 'min-h-screen' }, 'User menu fixture') }),
}).use(i18n).use(router).mount('#app')

document.documentElement.dataset.ready = 'true'
