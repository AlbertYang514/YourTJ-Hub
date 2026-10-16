import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { chromium, webkit } from 'playwright'
import { createServer } from 'vite'

let server, browser, origin
const browserType = process.env.PULL_REFRESH_BROWSER === 'webkit' ? webkit : chromium
before(async () => {
  server = await createServer({ root: fileURLToPath(new URL('../', import.meta.url)), server: { host: '127.0.0.1', port: 0, open: false } })
  await server.listen()
  origin = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await browserType.launch()
})
after(async () => { await browser?.close(); await server?.close() })

async function open(theme, reducedMotion, failRefresh = false) {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    reducedMotion: reducedMotion ? 'reduce' : 'no-preference',
  })
  const state = { requests: 0, release: undefined }
  await page.route('**/*', async (route) => {
    if (route.request().headers()['x-goose-page'] === 'true') {
      state.requests++
      if (!failRefresh) await new Promise((resolve) => { state.release = resolve })
      if (failRefresh) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 1, message: 'unavailable' }) })
      } else {
        await route.fulfill({ contentType: 'application/json', body: JSON.stringify(await page.evaluate(() => window.homePullRefreshPayload)) })
      }
      return
    }
    await route.continue()
  })
  await page.goto(`${origin}/assets/test/fixtures/browser/home-pull-refresh.html?theme=${theme}`)
  await page.waitForFunction(() => document.documentElement.dataset.ready === 'true')
  await page.locator('.gf-pull-refresh-logo').waitFor()
  await page.getByText('刷新前保留的话题', { exact: true }).waitFor()
  return { page, state }
}

async function dispatchPull(page, distance) {
  return page.evaluate((delta) => {
    const root = document.querySelector('#app')?.firstElementChild
    if (!root) throw new Error('HomePage root is missing')
    const event = (type, y) => {
      const value = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperty(value, 'touches', { value: type === 'touchend' ? [] : [{ clientY: y }] })
      root.dispatchEvent(value)
      return value
    }
    event('touchstart', 100)
    const move = event('touchmove', 100 + delta)
    event('touchend', 100 + delta)
    return move.defaultPrevented
  }, distance)
}

for (const theme of ['gf-light', 'gf-dark']) {
  for (const reducedMotion of [false, true]) {
    test(`${theme} with ${reducedMotion ? 'reduced' : 'normal'} motion keeps the branded pull contract`, async () => {
      const { page, state } = await open(theme, reducedMotion)
      try {
        const logo = page.locator('.gf-pull-refresh-logo')
        const logoStyle = await logo.evaluate((element) => {
          const rect = element.getBoundingClientRect()
          return {
            width: rect.width,
            height: rect.height,
            flexShrink: getComputedStyle(element).flexShrink,
            filter: getComputedStyle(element).filter,
            animationName: getComputedStyle(element).animationName,
            source: new URL(element.getAttribute('src'), location.href).pathname,
          }
        })
        assert.equal(logoStyle.width, 28)
        assert.equal(logoStyle.height, 28)
        assert.equal(logoStyle.flexShrink, '0')
        assert.match(logoStyle.source, /\/assets\/logo\.svg$/)
        assert.equal(logoStyle.filter, theme === 'gf-dark' ? 'brightness(0) invert(1)' : 'brightness(0)')
        assert.equal(logoStyle.animationName, 'none')
        assert.ok(await page.locator('.gf-pull-refresh-indicator').evaluate((el) => el.getBoundingClientRect().height >= 44))

        assert.equal(await dispatchPull(page, 100), true)
        await page.getByText('下拉刷新', { exact: true }).waitFor()
        await page.locator('.gf-pull-refresh-indicator').evaluate(async (el) => {
          await Promise.all(el.getAnimations().map((animation) => animation.finished))
        })
        assert.equal(state.requests, 0, 'a short pull must not refresh')

        await dispatchPull(page, 140)
        await page.waitForFunction(() => document.querySelector('.gf-pull-refresh-indicator')?.textContent?.includes('正在刷新'))
        assert.equal(state.requests, 1, 'release above the threshold starts one refresh')
        const inProgress = await logo.evaluate((element) => ({
          animationName: getComputedStyle(element).animationName,
          capsuleText: element.parentElement?.textContent?.trim(),
        }))
        assert.equal(inProgress.capsuleText, '正在刷新')
        assert.equal(inProgress.animationName === 'none', reducedMotion)

        state.release?.()
        await page.waitForFunction(() => document.querySelector('p[role="status"][aria-live="polite"]')?.textContent?.includes('已刷新到最新内容'))
        assert.equal(state.requests, 1)
      } finally {
        state.release?.()
        await page.close()
      }
    })
  }
}

test('failed PWA refresh retains the existing topic and reports failure', async () => {
  const { page, state } = await open('gf-light', true, true)
  try {
    await dispatchPull(page, 140)
    await page.waitForFunction(() => document.querySelector('p[role="status"][aria-live="polite"]')?.textContent?.includes('刷新失败'))
    assert.equal(state.requests, 1)
    await page.getByText('刷新前保留的话题', { exact: true }).waitFor()
  } finally {
    await page.close()
  }
})
