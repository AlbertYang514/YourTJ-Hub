// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TwemojiHtmlFixture from './fixtures/TwemojiHtmlFixture.vue'
import TwemojiTemplateFixture from './fixtures/TwemojiTemplateFixture.vue'
import { renderMarkdownPreview } from '../src/runtime/markdown'
import { parseTwemoji, renderTwemojiHtml, TWEMOJI_VERSION } from '../src/runtime/twemoji'

describe('shared Twemoji mapping', () => {
  it('keeps Unicode text and matches complete joined, regional, keycap, and tone sequences', () => {
    const text = '中文 A😀 🐦‍🔥 🇺🇸 1️⃣ 👩🏽‍💻!'
    const segments = parseTwemoji(text)

    expect(segments.filter((segment) => segment.type === 'emoji').map((segment) => segment.value)).toEqual([
      '😀',
      '🐦‍🔥',
      '🇺🇸',
      '1️⃣',
      '👩🏽‍💻',
    ])
    expect(segments.map((segment) => segment.value).join('')).toBe(text)
  })

  it('normalizes text variation selectors without changing the displayed Unicode', () => {
    expect(parseTwemoji('❤\uFE0F')).toEqual([
      { type: 'emoji', value: '❤\uFE0F', asset: '2764.png' },
    ])
  })

  it('leaves unsupported sequences as original text and pins the released artwork', () => {
    const unsupported = '🐦‍🛹'
    expect(parseTwemoji(unsupported)).toEqual([{ type: 'text', value: unsupported }])
    expect(TWEMOJI_VERSION).toBe('17.0.3')
  })
})

describe('Vue Twemoji rendering', () => {
  it('renders the Vditor preview after syntax highlighting without changing its structure', () => {
    const preview = document.createElement('div')
    preview.innerHTML = `<pre><code><span class="hljs-keyword">const</span> emoji = '🐦‍🔥'</code></pre>`

    renderTwemojiHtml(preview)

    expect(preview.querySelector('.hljs-keyword')?.textContent).toBe('const')
    expect(preview.querySelector('pre code img.twemoji')?.getAttribute('alt')).toBe('🐦‍🔥')
  })

  it('renders static and reactive text nodes with image alt text', () => {
    const wrapper = mount(TwemojiTemplateFixture, {
      props: { text: '中文 comment 🐦‍🔥!' },
    })

    const images = wrapper.findAll('img.twemoji')
    expect(images.map((image) => image.attributes('alt'))).toEqual([
      '😀',
      '🐦‍🔥',
      '🐦‍🔥',
    ])
    expect(images[1].attributes('src')).toContain(
      '/twemoji/72x72/1f426-200d-1f525.png',
    )
    expect(wrapper.find('code img.twemoji').attributes('alt')).toBe('🐦‍🔥')
  })

  it('preserves Markdown structure and renders Emoji inside code in v-html output', () => {
    const html = renderMarkdownPreview('**Markdown 🐦‍🔥** and `🔥`\n\n[Link ❤️](https://example.com)')
    const wrapper = mount(TwemojiHtmlFixture, { props: { html } })

    expect(wrapper.findAll('img.twemoji').map((image) => image.attributes('alt'))).toEqual([
      '🐦‍🔥',
      '🔥',
      '❤️',
    ])
    expect(wrapper.find('strong').exists()).toBe(true)
    expect(wrapper.find('a').attributes('href')).toBe('https://example.com')
    expect(wrapper.find('code img.twemoji').attributes('alt')).toBe('🔥')
  })

  it('falls back to source Unicode when an image cannot load', async () => {
    const wrapper = mount(TwemojiTemplateFixture, {
      props: { text: '中文 comment 🐦‍🔥!' },
    })

    for (const image of wrapper.findAll('img.twemoji')) {
      await image.trigger('error')
    }
    expect(wrapper.find('img.twemoji').exists()).toBe(false)
    expect(wrapper.text()).toContain('😀')
    expect(wrapper.text()).toContain('🐦‍🔥')
    wrapper.unmount()
  })

  it('falls back to source Unicode for a missing rich-HTML image', async () => {
    const wrapper = mount(TwemojiHtmlFixture, {
      props: { html: '<p>离线显示 🐦‍🔥</p>' },
    })

    await wrapper.find('img.twemoji').trigger('error')
    expect(wrapper.find('img.twemoji').exists()).toBe(false)
    expect(wrapper.find('p').text()).toBe('离线显示 🐦‍🔥')
    wrapper.unmount()
  })

  it('copies the original Unicode sequence when selected rich text includes an image', () => {
    const wrapper = mount(TwemojiHtmlFixture, {
      props: {
        html: '<p>复制 🐦‍🔥 and ❤️</p><pre><code>代码 🔥</code></pre><p>第二段 🧪</p>',
      },
    })
    const range = document.createRange()
    range.selectNodeContents(wrapper.element)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)

    const values: Record<string, string> = {}
    const event = new Event('copy', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', {
      value: { setData: (type: string, value: string) => (values[type] = value) },
    })
    document.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(values['text/plain']).toBe('复制 🐦‍🔥 and ❤️\n代码 🔥\n第二段 🧪')
    selection.removeAllRanges()
    wrapper.unmount()
  })
})
