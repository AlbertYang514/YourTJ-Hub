import { createApp, h, onMounted, ref } from 'vue'
import MarkdownImageViewer from '../../../src/components/MarkdownImageViewer.vue'
import { i18n } from '../../../src/runtime/i18n'
import { getMarkdownImagePreview } from '../../../src/runtime/markdown-image-preview'
import { renderTwemojiHtml, twemojiVuePlugin } from '../../../src/runtime/twemoji'
import '../../../src/styles/resource.css'

document.documentElement.dataset.theme = new URLSearchParams(location.search).get('theme') ?? 'gf-light'

const contentHtml = `
  <p id="mixed">before 🇺🇸, 👩🏽‍🚀, 🪿, and 🐦‍🔥 after</p>
  <p id="photo-line">ordinary photo <img id="photo" src="/photo.png" alt="Photo attachment" width="96" height="48"></p>
  <p id="copy">copy me 🇺🇸, 👩🏽‍🚀, 🪿, and 🐦‍🔥</p>
`

createApp({
  setup() {
    const content = ref<HTMLElement>()
    const viewer = ref<InstanceType<typeof MarkdownImageViewer> | null>(null)

    function handleMarkdownImageClick(event: MouseEvent) {
      const preview = getMarkdownImagePreview(event.target)
      if (!preview) return
      event.preventDefault()
      event.stopPropagation()
      viewer.value?.open(preview.images, preview.index)
    }

    onMounted(() => {
      if (content.value) renderTwemojiHtml(content.value)
    })

    return () => h('main', [
      h('div', {
        id: 'content',
        ref: content,
        class: 'gf-prose gf-prose-post',
        innerHTML: contentHtml,
        onClick: handleMarkdownImageClick,
      }),
      h(MarkdownImageViewer, { ref: viewer }),
    ])
  },
})
  .use(i18n)
  .use(twemojiVuePlugin)
  .mount('#app')
