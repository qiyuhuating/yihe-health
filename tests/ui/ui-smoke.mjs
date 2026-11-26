import fs from 'node:fs'
import path from 'node:path'

const [targetUrl, widthRaw, heightRaw, outputPath, mode = 'normal'] = process.argv.slice(2)
if (!targetUrl || !widthRaw || !heightRaw || !outputPath) {
  console.error('Usage: node tests/ui/ui-smoke.mjs <url> <width> <height> <screenshot> [normal|large-text]')
  process.exit(2)
}

const width = Number(widthRaw)
const height = Number(heightRaw)
// The target URL already tells us which route must survive bootstrap redirects.
// Deriving it here keeps the assertion from drifting when routes are renamed.
const expectedHash = new URL(targetUrl).hash
if (!expectedHash) {
  console.error('Target URL must contain a hash route, for example http://127.0.0.1:4173/#/sign-in')
  process.exit(2)
}
const port = Number(process.env.CDP_PORT || 9222)
const endpoint = `http://127.0.0.1:${port}`

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))

async function waitForChrome () {
  let lastError
  for (let i = 0; i < 80; i++) {
    try {
      const response = await fetch(`${endpoint}/json/version`)
      if (response.ok) return
    } catch (error) {
      lastError = error
    }
    await sleep(250)
  }
  throw lastError || new Error('Chrome DevTools endpoint did not become ready')
}

async function openTarget () {
  const response = await fetch(`${endpoint}/json/new?${encodeURIComponent(targetUrl)}`, { method: 'PUT' })
  if (!response.ok) throw new Error(`Unable to open Chrome target: ${response.status}`)
  return response.json()
}

class CdpClient {
  constructor (socket) {
    this.socket = socket
    this.nextId = 1
    this.pending = new Map()
    socket.addEventListener('message', event => {
      const message = JSON.parse(String(event.data))
      if (!message.id) return
      const pending = this.pending.get(message.id)
      if (!pending) return
      this.pending.delete(message.id)
      if (message.error) pending.reject(new Error(JSON.stringify(message.error)))
      else pending.resolve(message.result || {})
    })
  }

  send (method, params = {}) {
    const id = this.nextId++
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.socket.send(JSON.stringify({ id, method, params }))
    })
  }
}

async function connect (webSocketDebuggerUrl) {
  const socket = new WebSocket(webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  return { socket, client: new CdpClient(socket) }
}

async function waitForDocument (client) {
  for (let i = 0; i < 100; i++) {
    const result = await client.send('Runtime.evaluate', {
      expression: 'document.readyState === "complete"',
      returnByValue: true
    })
    if (result.result?.value === true) {
      await client.send('Runtime.evaluate', {
        expression: 'document.fonts && document.fonts.ready ? document.fonts.ready.then(() => true) : true',
        awaitPromise: true,
        returnByValue: true
      })
      await sleep(700)
      return
    }
    await sleep(150)
  }
  throw new Error('Document did not become ready')
}

function auditAccessibilityTree (tree) {
  const namedRoles = new Set([
    'button', 'link', 'textbox', 'searchbox', 'checkbox', 'radio',
    'combobox', 'switch', 'slider', 'spinbutton', 'heading'
  ])
  const violations = []

  for (const node of tree.nodes || []) {
    if (node.ignored) continue
    const role = node.role?.value
    const name = String(node.name?.value || '').trim()
    if (namedRoles.has(role) && !name) {
      violations.push(`${role} has no accessible name (backendDOMNodeId=${node.backendDOMNodeId || 'unknown'})`)
    }
  }
  return violations
}

async function auditTargetSizes (client) {
  const expression = `(() => {
    const selector = [
      'button',
      'a[href]',
      'input:not([type="hidden"])',
      'select',
      'textarea',
      '[role="button"]',
      '[role="link"]',
      '[tabindex]:not([tabindex="-1"])'
    ].join(',')

    const visible = el => {
      const style = getComputedStyle(el)
      const rect = el.getBoundingClientRect()
      return style.visibility !== 'hidden' &&
        style.display !== 'none' &&
        Number(style.opacity || 1) !== 0 &&
        rect.width > 0 &&
        rect.height > 0
    }

    const label = el =>
      el.getAttribute('aria-label') ||
      el.getAttribute('title') ||
      el.textContent?.trim() ||
      el.getAttribute('placeholder') ||
      el.tagName.toLowerCase()

    return [...new Set(document.querySelectorAll(selector))]
      .filter(visible)
      .map(el => {
        const rect = el.getBoundingClientRect()
        return {
          tag: el.tagName.toLowerCase(),
          label: label(el).slice(0, 80),
          width: Math.round(rect.width * 10) / 10,
          height: Math.round(rect.height * 10) / 10
        }
      })
      .filter(item => item.width < 44 || item.height < 44)
  })()`

  const result = await client.send('Runtime.evaluate', {
    expression,
    returnByValue: true
  })
  return result.result?.value || []
}

async function main () {
  await waitForChrome()
  const target = await openTarget()
  const { socket, client } = await connect(target.webSocketDebuggerUrl)

  try {
    await client.send('Page.enable')
    await client.send('Runtime.enable')
    await client.send('Accessibility.enable')
    await client.send('Emulation.setDeviceMetricsOverride', {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: width <= 760
    })
    await client.send('Page.navigate', { url: targetUrl })
    await waitForDocument(client)

    if (mode === 'large-text') {
      await client.send('Runtime.evaluate', {
        expression: 'localStorage.setItem("large_text", "true")'
      })
      await client.send('Page.reload', { ignoreCache: true })
      await waitForDocument(client)
    }

    const location = await client.send('Runtime.evaluate', {
      expression: 'location.href',
      returnByValue: true
    })
    if (!String(location.result?.value || '').endsWith(expectedHash)) {
      throw new Error(`Expected ${expectedHash} route, got ${location.result?.value}`)
    }

    const tree = await client.send('Accessibility.getFullAXTree')
    const accessibleNameViolations = auditAccessibilityTree(tree)
    const undersizedTargets = await auditTargetSizes(client)

    const screenshot = await client.send('Page.captureScreenshot', {
      format: 'png',
      fromSurface: true,
      captureBeyondViewport: false
    })
    fs.mkdirSync(path.dirname(outputPath), { recursive: true })
    fs.writeFileSync(outputPath, Buffer.from(screenshot.data, 'base64'))

    console.log(JSON.stringify({
      url: location.result?.value,
      viewport: { width, height },
      mode,
      screenshot: outputPath,
      accessibleNameViolations,
      undersizedTargets
    }, null, 2))

    if (accessibleNameViolations.length || undersizedTargets.length) {
      process.exitCode = 1
    }
  } finally {
    socket.close()
  }
}

main().catch(error => {
  console.error(error)
  process.exit(1)
})
