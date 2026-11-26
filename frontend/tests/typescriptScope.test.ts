// @vitest-environment node
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import { afterEach, expect, it } from 'vitest'

const roots: string[] = []
const gate = fileURLToPath(new URL('../scripts/check-typescript-scope.ts', import.meta.url))
function fixture() {
  const root = mkdtempSync(path.join(tmpdir(), 'yihe-sfc-scope-'))
  roots.push(root)
  mkdirSync(path.join(root, 'src'))
  mkdirSync(path.join(root, 'scripts'))
  const config = { compilerOptions: { strict: true, allowJs: false }, include: ['src/**/*.ts', 'src/**/*.vue'] }
  writeFileSync(path.join(root, 'tsconfig.app.json'), JSON.stringify(config))
  writeFileSync(path.join(root, 'src/entry.ts'), 'export const value = 1')
  writeFileSync(path.join(root, 'src/App.vue'), '<template><div /></template><script setup lang="ts">const value = 1</script>')
  return { root, config }
}
function check(root: string) {
  return spawnSync(process.execPath, [gate, root], { encoding: 'utf8', windowsHide: true, timeout: 30000 })
}
afterEach(() => {
  for (const root of roots.splice(0)) {
    if (path.dirname(root) !== path.resolve(tmpdir()) || !path.basename(root).startsWith('yihe-sfc-scope-')) throw new Error('Unsafe fixture path')
    rmSync(root, { recursive: true, force: true })
  }
})

it('checks typed SFCs against the real vue-tsc CLI scope', () => {
  const { root } = fixture()
  expect(check(root).status).toBe(0)
})
it('rejects a previously typed component silently switched to JS', () => {
  const { root } = fixture()
  writeFileSync(path.join(root, 'src/App.vue'), '<script setup>const value = 1</script>')
  expect(check(root).stderr).toContain('must use inline lang="ts"')
})
it('tracks existing JS debt explicitly without claiming it was type-checked', () => {
  const { root } = fixture()
  writeFileSync(path.join(root, 'src/App.vue'), '<script setup>const value = 1</script>')
  writeFileSync(path.join(root, 'scripts/sfc-typecheck-baseline.json'), JSON.stringify({ untyped: ['src/App.vue'] }))
  const result = check(root)
  expect(result.status).toBe(0)
  expect(result.stdout).toContain('1 existing JS scripts tracked for migration')
})
it('requires a migrated component to be removed from the debt list', () => {
  const { root } = fixture()
  writeFileSync(path.join(root, 'scripts/sfc-typecheck-baseline.json'), JSON.stringify({ untyped: ['src/App.vue'] }))
  expect(check(root).stderr).toContain('remove migrated SFC')
})
it('rejects standalone JavaScript and typed files omitted from compiler inputs', () => {
  const { root } = fixture()
  writeFileSync(path.join(root, 'src/hidden.js'), 'export default 1')
  writeFileSync(path.join(root, 'src/hidden.tsx'), 'export const hidden = 1')
  const result = check(root)
  expect(result.status).toBe(1)
  expect(result.stderr).toContain('JavaScript module bypasses TS')
  expect(result.stderr).toContain('source absent from compiler scope')
})
it('rejects a typed component excluded by inherited tsconfig settings', () => {
  const { root, config } = fixture()
  writeFileSync(path.join(root, 'base.json'), JSON.stringify({ exclude: ['src/App.vue'] }))
  writeFileSync(path.join(root, 'tsconfig.app.json'), JSON.stringify({ ...config, extends: './base.json' }))
  expect(check(root).stderr).toContain('source absent from compiler scope')
})
