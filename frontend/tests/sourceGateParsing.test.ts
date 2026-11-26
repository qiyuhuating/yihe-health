// @vitest-environment node
import { createRequire } from 'node:module'
import { spawnSync } from 'node:child_process'
import { expect, it } from 'vitest'
import { inlineScripts } from '../scripts/sfc-scripts.ts'

const require = createRequire(import.meta.url)
const utilityPath = require.resolve('../vendor/secured-picomatch/lib/utils.js')
const { removeBackslashes } = require(utilityPath) as { removeBackslashes(input: string): string }

it('inspects both compiler-recognized script blocks with closing-tag whitespace', () => {
  const blocks = inlineScripts('<script lang="ts">export const value = 1</script >\n<script setup lang="ts">import value from "./other"</script\n>', 'fixture.vue')
  expect(blocks.map(block => block.lang)).toEqual(['ts', 'ts'])
  expect(blocks.map(block => block.content)).toEqual(['export const value = 1', 'import value from "./other"'])
})

it('ignores script-like comments and rejects malformed or external scripts', () => {
  expect(inlineScripts('<!-- <script>new Vue()</script> --><template><div /></template>', 'fixture.vue')).toEqual([])
  expect(() => inlineScripts('<script setup lang="ts">const value = 1', 'broken.vue')).toThrow('Invalid SFC')
  expect(() => inlineScripts('<script src="./hidden.ts" lang="ts"></script>', 'external.vue')).toThrow('External SFC script')
})

it('preserves bracket classes, trailing escapes and line terminators when unescaping', () => {
  const cases: [string, string][] = [
    ['a\\*b', 'a*b'], ['a\\', 'a\\'], ['[a\\*b]', '[a\\*b]'],
    ['[a\\]b]', '[a\\]b]'], ['[a\\]', '[a]'], ['[]\\*', '[]*'],
    ['[\n]\\*', '[\n]*'], ['[\n\\*]', '[\n*]'],
    ['\\\r', '\\\r'], ['\\\u2028', '\\\u2028'], ['\\\u2029', '\\\u2029']
  ]
  for (const [input, expected] of cases) expect(removeBackslashes(input)).toBe(expected)
})

it('handles a long unmatched bracket sequence within a bounded child process', () => {
  const result = spawnSync(process.execPath, ['-e',
    'const u=require(process.argv[1]);const x="[".repeat(250000);if(u.removeBackslashes(x)!==x)process.exit(2)', utilityPath],
  { encoding: 'utf8', windowsHide: true, timeout: 2000 })
  expect(result.error).toBeUndefined()
  expect(result.status).toBe(0)
})
