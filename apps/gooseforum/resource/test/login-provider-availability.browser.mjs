import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { createServer } from 'vite'

let server, browser, origin
before(async () => {
  server = await createServer({ root: fileURLToPath(new URL('../', import.meta.url)), server: { host: '127.0.0.1', port: 0, open: false } })
  await server.listen()
  origin = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await chromium.launch()
})
after(async () => { await browser?.close(); await server?.close() })

const unavailable = {
  zh: { github: 'GitHub 暂未开放', google: 'Google 暂未开放' },
  en: { github: 'GitHub is not available yet', google: 'Google is not available yet' },
  ja: { github: 'GitHub はまだ利用できません', google: 'Google はまだ利用できません' },
  de: { github: 'GitHub ist noch nicht verfügbar', google: 'Google ist noch nicht verfügbar' },
}

for (const lang of Object.keys(unavailable)) for (const width of [320, 1280]) for (const theme of ['gf-light', 'gf-dark']) {
  test(`unconfigured OAuth controls are disabled, localized and visible: ${lang}, ${width}px, ${theme}`, async () => {
    const page = await browser.newPage({ viewport: { width, height: 950 } })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    try {
      await page.goto(`${origin}/assets/test/fixtures/browser/tongji-login.html?lang=${lang}&mode=login&github=off&google=off&theme=${theme}`)
      await page.locator('html[data-ready="true"]').waitFor()

      for (const provider of ['github', 'google']) {
        const control = page.locator(`[data-oauth-provider="${provider}"]:visible`)
        await control.waitFor()
        assert.equal(await control.count(), 1)
        assert.equal(await control.evaluate(element => element.tagName), 'BUTTON')
        assert.equal(await control.isDisabled(), true)
        assert.equal(await control.getAttribute('href'), null)
        assert.equal((await control.textContent()).trim(), unavailable[lang][provider])

        const accessible = await control.ariaSnapshot()
        assert.ok(accessible.includes(unavailable[lang][provider]), accessible)
        assert.match(accessible, /disabled/i)

        const visual = await control.evaluate(element => {
          const style = getComputedStyle(element)
          const box = element.getBoundingClientRect()
          element.focus()
          return {
            display: style.display,
            opacity: style.opacity,
            color: style.color,
            width: box.width,
            left: box.left,
            right: box.right,
            focused: document.activeElement === element,
          }
        })
        assert.notEqual(visual.display, 'none')
        assert.equal(visual.opacity, '0.7')
        assert.notEqual(visual.color, 'rgba(0, 0, 0, 0)')
        assert.ok(visual.width >= 40 && visual.left >= 0 && visual.right <= width + 1)
        assert.equal(visual.focused, false, 'a disabled login control must not receive keyboard focus')
      }

      assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), theme)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
      assert.deepEqual(errors, [])
    } finally { await page.close() }
  })
}

for (const width of [320, 1280]) {
  test(`configured GitHub entry remains keyboard-accessible at ${width}px`, async () => {
    const page = await browser.newPage({ viewport: { width, height: 950 } })
    try {
      await page.goto(`${origin}/assets/test/fixtures/browser/tongji-login.html?lang=en&mode=login&google=off&theme=gf-light`)
      await page.locator('html[data-ready="true"]').waitFor()
      const link = page.locator('[data-oauth-provider="github"]:visible')
      await link.waitFor()
      assert.equal(await link.count(), 1)
      assert.equal(await link.evaluate(element => element.tagName), 'A')
      assert.equal(await link.getAttribute('href'), '/api/auth/github')
      assert.match(await link.ariaSnapshot(), /link "GitHub"/i)

      let reachableByTab = false
      for (let attempt = 0; attempt < 24; attempt++) {
        await page.keyboard.press('Tab')
        if (await link.evaluate(element => document.activeElement === element)) {
          reachableByTab = true
          break
        }
      }
      assert.equal(reachableByTab, true, 'the configured OAuth link must participate in keyboard navigation')
    } finally { await page.close() }
  })
}
