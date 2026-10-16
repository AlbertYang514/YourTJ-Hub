import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { createServer } from 'vite'

let server
let browser
let origin

before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('../', import.meta.url)),
    server: { host: '127.0.0.1', port: 0, open: false },
  })
  await server.listen()
  origin = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await chromium.launch()
})

after(async () => {
  await browser?.close()
  await server?.close()
})

const locales = {
  zh: { cancel: '取消', empty: '还没有草稿' },
  en: { cancel: 'Cancel', empty: 'No drafts yet' },
  ja: { cancel: 'キャンセル', empty: '下書きはまだありません' },
  de: { cancel: 'Abbrechen', empty: 'Noch keine Entwürfe' },
}

for (const [locale, copy] of Object.entries(locales)) {
  test(`草稿删除确认可用键盘与触屏并正确更新空状态（${locale}，Chromium）`, async () => {
    const page = await browser.newPage({
      viewport: { width: 360, height: 780 },
      isMobile: true,
      hasTouch: true,
    })
    page.setDefaultTimeout(8_000)
    page.setDefaultNavigationTimeout(8_000)
    let deletionCount = 0
    let deleteBody
    try {
      await page.route('**/api/forum/user/content-event', route => route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, result: true }),
      }))
      await page.route('**/api/forum/topics/delete', async route => {
        deletionCount++
        deleteBody = route.request().postDataJSON()
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ code: 0, result: true }),
        })
      })

      await page.goto(`${origin}/assets/test/fixtures/browser/drafts-page.html?lang=${locale}`)
      const deleteButton = page.getByRole('button', { name: /A saved draft/ })
      const editLink = page.getByRole('link', { name: /A saved draft/ }).first()
      await deleteButton.waitFor()
      await editLink.waitFor()
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
      assert.ok((await deleteButton.boundingBox()).height >= 40)

      await deleteButton.focus()
      await page.keyboard.press('Enter')
      const dialog = page.getByRole('alertdialog')
      await dialog.waitFor()
      assert.match(await dialog.textContent(), /A saved draft/)
      assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('data-test')), 'cancel-delete')
      await page.keyboard.press('Escape')
      await dialog.waitFor({ state: 'detached' })
      assert.equal(deletionCount, 0)

      const buttonBox = await deleteButton.boundingBox()
      await page.touchscreen.tap(buttonBox.x + buttonBox.width / 2, buttonBox.y + buttonBox.height / 2)
      await dialog.waitFor()
      await page.locator('[data-test="confirm-delete"]').focus()
      await page.keyboard.press('Enter')
      await page.getByText(copy.empty, { exact: true }).waitFor()

      assert.equal(deletionCount, 1)
      assert.equal(deleteBody.topicId, 42)
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
      assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('href')), '/publish')
    } finally {
      await page.close()
    }
  })
}
