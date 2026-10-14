// Screenshots of the demo stack (run by shoot.sh inside zenika/alpine-chrome:with-puppeteer).
//
//   node shot.js <outdir> <shots.json>
//
// shots.json: [{ "name": "home", "path": "/", "width": 1440, "height": 1100,
//                "themes": ["light", "dark"], "scroll": "#h-links", "wait": 2500,
//                "type": [{ "sel": "input[placeholder=any]", "text": "123" }],
//                "click": "text to click" }]
// Each shot is a viewport-sized capture (not a full page): `type` fills
// inputs, `click` presses the first button/link whose text matches, then
// `scroll` puts an element at the top.
const fs = require('fs')
const puppeteer = require('puppeteer')

const out = process.argv[2]
const shots = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'))
const base = process.env.BASE || 'http://asdemo-web'

;(async () => {
  const browser = await puppeteer.launch({
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu', '--font-render-hinting=none'],
    protocolTimeout: 180000,
  })
  for (const s of shots) {
    for (const theme of s.themes || ['light', 'dark']) {
      const p = await browser.newPage()
      p.on('pageerror', (e) => console.log(`PAGEERROR ${s.name} ${theme}: ${e.message}`))
      p.on('console', (m) => {
        if (m.type() === 'error') console.log(`CONSOLE ${s.name}: ${m.text().slice(0, 200)}`)
      })
      const width = s.width || 1440
      const height = s.height || 1000
      await p.setViewport({ width, height, deviceScaleFactor: s.scale || 1 })
      await p.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: theme }])
      await p.evaluateOnNewDocument((t) => {
        try { localStorage.setItem('theme', t) } catch (e) { /* storage blocked */ }
      }, theme)
      await p.goto(base + s.path, { waitUntil: 'networkidle0', timeout: 120000 })
      await p.evaluate(() => document.fonts.ready)
      for (const t of s.type || []) {
        await p.type(t.sel, t.text)
      }
      if (s.click) {
        const clicked = await p.evaluate((txt) => {
          const el = [...document.querySelectorAll('button, a, [role=tab]')].find((e) => e.textContent.trim() === txt)
          if (el) el.click()
          return !!el
        }, s.click)
        if (!clicked) console.log(`NOCLICK ${s.name}: ${s.click}`)
        await p.waitForNetworkIdle({ idleTime: 800, timeout: 60000 }).catch(() => {})
      }
      if (s.scroll) {
        // a CSS selector, or the exact text of a heading
        const found = await p.evaluate((sel) => {
          let el = null
          try { el = document.querySelector(sel) } catch (e) { /* not a selector */ }
          if (!el) el = [...document.querySelectorAll('h1, h2, h3')].find((h) => h.textContent.trim() === sel)
          if (el) window.scrollTo(0, el.getBoundingClientRect().top + window.scrollY - 88)
          return !!el
        }, s.scroll)
        if (!found) console.log(`NOSCROLL ${s.name}: ${s.scroll}`)
      }
      await p.evaluate(() => document.activeElement && document.activeElement.blur())
      await new Promise((r) => setTimeout(r, s.wait || 2500))
      const sw = await p.evaluate(() => document.documentElement.scrollWidth)
      if (sw > width) console.log(`OVERFLOW ${s.name} ${theme}: scrollWidth ${sw}`)
      const file = `${out}/${s.name}-${theme}.png`
      await p.screenshot({ path: file })
      console.log(`wrote ${file}`)
      await p.close()
    }
  }
  await browser.close()
})()
