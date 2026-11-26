import { describe, expect, it } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { SRC } from '../helpers/sourceTree'

// A detail view that survives a route-key change must retire every in-flight
// write and the local state it would publish. The source tree decides which
// views are in scope: no file list, no allowlist, no count to maintain.
const ROUTE_WATCH = /watch\s*\(\s*\(\s*\)\s*=>\s*route\.(?:query|params)\./
const WRITE_CALL = /api\.post\s*(?:<[^>]*>)?\s*\(/

function balancedBodies(source: string, pattern: RegExp): { name: string; text: string }[] {
  const bodies: { name: string; text: string }[] = []
  let match: RegExpExecArray | null
  pattern.lastIndex = 0
  while ((match = pattern.exec(source))) {
    let depth = 1
    let end = pattern.lastIndex
    for (; end < source.length && depth > 0; end++) {
      const char = source[end]
      if (char === '{') depth++
      else if (char === '}') depth--
      else if (char === '"' || char === "'" || char === '`') {
        const quote = char
        end++
        while (end < source.length && source[end] !== quote) {
          if (source[end] === '\\') end++
          end++
        }
      }
    }
    bodies.push({ name: match[1], text: source.slice(match.index, end) })
    pattern.lastIndex = end
  }
  return bodies
}

describe('route-scoped write guard', () => {
  it('invalidates write state whenever a watched route key changes', () => {
    const directory = path.join(SRC, 'views')
    const files = fs.readdirSync(directory).filter(name => name.endsWith('.vue'))
    expect(files.length).toBeGreaterThan(0)
    const violations: string[] = []
    let guarded = 0
    for (const name of files) {
      const source = fs.readFileSync(path.join(directory, name), 'utf8')
      if (!ROUTE_WATCH.test(source)) continue
      // Read-only detail pages have their posts inside abortable readers, not
      // inside async write handlers, so they are out of scope.
      const writers = balancedBodies(source, /async function (\w+)\s*\([^)]*\)\s*\{/g)
        .filter(body => WRITE_CALL.test(body.text))
      if (!writers.length) continue
      guarded++
      const generation = source.match(/\blet\s+(\w*[Rr]evision)\s*=\s*0\b/)
      if (!generation) {
        violations.push(`${name}: no write generation declared`)
        continue
      }
      const key = generation[1]
      if (!new RegExp(`\\b${key}\\+\\+`).test(source)) {
        violations.push(`${name}: write generation is never invalidated`)
      }
      for (const writer of writers) {
        if (!new RegExp(`const revision = ${key}\\b`).test(writer.text) ||
            !new RegExp(`revision\\s*(?:!==|===)\\s*${key}\\b`).test(writer.text)) {
          violations.push(`${name}: ${writer.name} can publish a stale write`)
        }
      }
    }
    expect(guarded).toBeGreaterThan(0)
    expect(violations).toEqual([])
  })
})
