import { h } from 'vue'
import { createBrowserApp as createApp } from './create-app'
import { createMemoryHistory, createRouter } from 'vue-router'
import HomePage from '../../../src/site/pages/HomePage.vue'
import { i18n, setLocale } from '../../../src/runtime/i18n'
import type { HomeProps, LayoutPayload, PagePayload } from '@gooseforum/client'
import '../../../src/styles/resource.css'

await setLocale('zh')

const query = new URLSearchParams(location.search)
document.documentElement.dataset.theme = query.get('theme') || 'gf-light'
const nativeMatchMedia = window.matchMedia.bind(window)
window.matchMedia = ((media: string) => {
  const queryList = nativeMatchMedia(media)
  if (media === '(display-mode: standalone)' || media === '(pointer: coarse)') {
    return new Proxy(queryList, {
      get(target, property, receiver) {
        return property === 'matches' ? true : Reflect.get(target, property, receiver)
      },
    })
  }
  return queryList
}) as typeof window.matchMedia

const topics: HomeProps['topics'] = [{
  id: 101,
  title: '刷新前保留的话题',
  description: '用于确认刷新失败时原有内容仍在。',
  url: '/p/post/101',
  author: { id: 202, username: 'tester', avatarUrl: '' },
  participants: [],
  categories: [],
  replyCount: 0,
  likeCount: 0,
  viewCount: 1,
  activityText: '刚刚',
  lastUpdateTime: '2026-10-16T00:00:00Z',
  pinWeight: 0,
  processStatus: 0,
  contentType: 0,
  liked: false,
  bookmarked: false,
} as HomeProps['topics'][number]]

const props: HomeProps = {
  sort: 'latest',
  tabs: [{ key: 'latest', url: '/', active: true }],
  topics,
  pagination: { page: 1, nextPage: 0, hasNext: false, nextUrl: '' },
  announcement: { enabled: false, html: '', items: [] },
}
const layout = {
  viewer: {
    id: 12,
    username: 'tester',
    isAuthenticated: true,
    requiresEmailVerification: false,
  },
} as LayoutPayload
const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { render: () => null } }],
})
await router.push('/')
await router.isReady()

const refreshPayload: PagePayload<HomeProps> = {
  component: 'home.index',
  url: '/',
  version: '1.0',
  layout,
  props: { ...props, topics: [] },
}
Object.assign(window, { homePullRefreshPayload: refreshPayload })

createApp({ render: () => h(HomePage, { layout, props, pageUrl: '/' }) })
  .use(i18n)
  .use(router)
  .mount('#app')
document.documentElement.dataset.ready = 'true'
