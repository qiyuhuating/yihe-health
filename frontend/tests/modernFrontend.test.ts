import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

// This file lives in tests/, but every assertion below is anchored to the frontend root.
const root = dirname(dirname(fileURLToPath(import.meta.url)))
const source = join(root, 'src')

function* sourceFiles(dir: string): Generator<string> {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) yield* sourceFiles(path)
    else if (/\.(vue|css|ts|js)$/.test(entry.name)) yield path
  }
}

it('keeps Vue 2 component patterns and icon fonts out of the current frontend', () => {
  const banned = [
    /\bel-icon-[a-z]+(?:-[a-z]+)*\b/,
    /\belement-icons\b/,
    /\bmixins\s*:/,
    /\bcustom-class\s*=/,
    /\bVue\.prototype\b/,
    /\bnew\s+Vue\s*\(/,
    /\b(?:beforeDestroy|destroyed)\s*\(/,
    /\bconfig\.globalProperties\s*\.\s*\$(?:message|alert|confirm|store|uploadHeaders)\b/
  ]
  for (const file of sourceFiles(source)) {
    const text = readFileSync(file, 'utf8')
    const name = relative(root, file)
    for (const pattern of banned) {
      expect(pattern.test(text), `${name} still contains ${pattern}`).toBe(false)
    }
    if (file.endsWith('.vue') && /<script\b/.test(text)) {
      expect(/<script\s+setup\b/.test(text), `${name} must use script setup`).toBe(true)
    }
  }
  expect(existsSync(join(root, 'src/assets/css/icons.css'))).toBe(false)
  expect(existsSync(join(root, 'public/static/icons/element-icons.woff'))).toBe(false)
  expect(readFileSync(join(root, 'index.html'), 'utf8')).toContain('/src/main.ts')
})
