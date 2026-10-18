import { h } from 'vue'
import { createBrowserApp as createApp } from './create-app'
import DraftsPage from '../../../src/site/pages/DraftsPage.vue'
import { i18n, setLocale } from '../../../src/runtime/i18n'
import '../../../src/styles/resource.css'

const params = new URLSearchParams(location.search)
await setLocale(params.get('lang') ?? 'zh')
document.documentElement.dataset.theme = params.get('theme') || 'gf-light'

createApp({
  render: () => h(DraftsPage, {
    layout: {} as any,
    props: {
      drafts: [{
        id: 42,
        title: 'A saved draft',
        description: 'Draft body preview',
        editUrl: '/publish?id=42',
        replyCount: 0,
        viewCount: 0,
        processStatus: 0,
        updatedAt: '2026-10-16T00:00:00Z',
        createdAt: '2026-10-15T00:00:00Z',
        categories: [],
      }],
      total: 1,
      pagination: { page: 1, nextPage: 0, hasNext: false, nextUrl: '' },
    } as any,
  }),
}).use(i18n).mount('#app')
