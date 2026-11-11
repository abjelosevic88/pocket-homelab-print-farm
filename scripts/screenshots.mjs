// README screenshots and a browser smoke test against a demo instance:
//
//   make build && DATA_DIR=/tmp/ppf-demo DEMO_DATA=true AUTH_MODE=none HOST=127.0.0.1 PORT=18274 ./pocket-printfarm &
//   CDP=http://127.0.0.1:9344 BASE=http://127.0.0.1:18274 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18274'
const shots = process.env.SHOTS !== '0'
const only = process.env.ONLY || ''
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false, w = 1440, h = 900 } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: w, height: h },
    deviceScaleFactor: 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|ERR_/.test(m.text())) errors.push(m.text()) })
  return { ctx, page }
}
async function go(page, path, wait = 1400) {
  await page.goto(base + path, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(wait)
}
async function shot(page, name) {
  if (!shots || (only && !only.split(',').includes(name))) return
  for (let i = 0; ; i++) {
    try { await page.bringToFront(); await page.screenshot({ path: dir + name + '.png', animations: 'disabled' }); return } catch (e) { if (i >= 3) throw e; await page.waitForTimeout(700) }
  }
}
const api = (page, path, method = 'GET', body) => page.evaluate(([p, m, b]) => fetch('/api/v1' + p, { method: m, headers: { 'Content-Type': 'application/json' }, body: b ? JSON.stringify(b) : undefined }).then(r => r.status === 204 ? {} : r.json()), [path, method, body])
const has = async (page, text, what) => assert.ok((await page.content()).includes(text), what)

const { page } = await session()
await go(page, '/', 2200)
await has(page, 'Twisted vase', 'the farm shows the print on the MK4')
await has(page, 'Desk planter', 'the farm shows the print on the Voron')
await shot(page, 'desktop-farm')

await go(page, '/queue')
await has(page, 'Phone stand', 'the queue')
await shot(page, 'desktop-queue')

await go(page, '/models', 1800)
await has(page, 'Gear, 24 teeth', 'the model library')
await shot(page, 'desktop-models')

const models = (await api(page, '/models?limit=100')).models
const vase = models.find(m => m.name === 'Twisted vase')
await go(page, `/models/${vase.id}`, 2600)
await has(page, 'What the slicer said', 'the model page')
await shot(page, 'desktop-model')

await go(page, '/filament', 1600)
await has(page, 'Galaxy Black', 'the spools')
await shot(page, 'desktop-filament')

await go(page, '/prints')
await has(page, 'History', 'the history')
const prints = (await api(page, '/prints?status=failed&limit=5')).prints
assert.ok(prints.length > 0, 'some prints failed')
await go(page, `/prints/${prints[0].id}`)
await has(page, 'What it cost', 'a print with its cost')
await shot(page, 'desktop-print')

await go(page, '/stats', 1600)
await has(page, 'Why prints failed', 'the numbers')
await shot(page, 'desktop-stats')

const printers = (await api(page, '/printers')).printers
await go(page, `/printers/${printers[1].id}`)
await has(page, 'Log book', 'a printer page')
await shot(page, 'desktop-printer')

await go(page, '/settings/apps')
await has(page, 'Pocket Inventory', 'the connected apps')

// the flow: start a print by hand and finish it
const job = (await api(page, '/jobs')).jobs.find(j => j.title === 'Drawer knob')
const ender = printers.find(p => p.name === 'Ender')
const started = await api(page, `/jobs/${job.id}/start`, 'POST', { printer_id: ender.id, force: true })
assert.equal(started.status, 'printing', 'a job starts on a printer')
const done = await api(page, `/prints/${started.id}/finish`, 'POST', { status: 'done', rating: 5 })
assert.equal(done.status, 'done', 'and is done')
assert.ok(done.grams > 0 && done.cost > 0, 'with grams and a cost')

const dark = await session({ dark: true })
await go(dark.page, '/', 2200)
await shot(dark.page, 'desktop-farm-dark')
await dark.ctx.close()

const phone = await session({ phone: true })
await go(phone.page, '/', 2200)
await shot(phone.page, 'phone-farm')
await go(phone.page, '/filament', 1600)
await shot(phone.page, 'phone-filament')
await go(phone.page, `/models/${vase.id}`, 2600)
await shot(phone.page, 'phone-model')
await go(phone.page, '/queue')
await shot(phone.page, 'phone-queue')
await phone.ctx.close()

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok: browser smoke passed' + (shots ? ', screenshots in ' + dir : ''))
