import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import { parse } from 'vue/compiler-sfc'

const root = path.resolve(process.argv[2] || fileURLToPath(new URL('..', import.meta.url)))
const compiler = fileURLToPath(new URL('../node_modules/vue-tsc/bin/vue-tsc.js', import.meta.url))
// Delegate configuration inheritance and file expansion to the real CLI. This
// gate only checks source coverage and SFC language, not TypeScript semantics.
const result = spawnSync(process.execPath, [compiler, '--showConfig', '-p', path.join(root, 'tsconfig.app.json')], {
  encoding: 'utf8', windowsHide: true, timeout: 30000, maxBuffer: 8 * 1024 * 1024
})
if (result.status !== 0) throw new Error(result.stderr || result.stdout || String(result.error))
const project = JSON.parse(result.stdout) as { files?: string[] }
const included = new Set((project.files ?? []).map(file => path.resolve(root, file)))
const baselineFile = path.join(root, 'scripts/sfc-typecheck-baseline.json')
const baseline = fs.existsSync(baselineFile)
  ? JSON.parse(fs.readFileSync(baselineFile, 'utf8')) as { untyped: string[] }
  : { untyped: [] as string[] }
const legacy = new Set<string>(baseline.untyped)
const remaining = new Set(legacy)
const problems: string[] = []
let components = 0
let typedScripts = 0
let templateOnly = 0
function inspect(directory: string): void {
  for (const item of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, item.name)
    const relative = path.relative(root, file).split(path.sep).join('/')
    if (item.isSymbolicLink()) { problems.push(`source symlink requires review: ${relative}`); continue }
    if (item.isDirectory()) { inspect(file); continue }
    if (/\.(?:[cm]?js|jsx)$/.test(item.name)) problems.push(`JavaScript module bypasses TS: ${relative}`)
    if (/\.(?:[cm]?ts|tsx|vue)$/.test(item.name) && !included.has(file) && !legacy.has(relative)) {
      problems.push(`source absent from compiler scope: ${relative}`)
    }
    if (!item.name.endsWith('.vue')) continue
    components++
    const { descriptor, errors } = parse(fs.readFileSync(file, 'utf8'), { filename: file })
    if (errors.length) problems.push(`invalid SFC: ${relative}`)
    const scripts = [descriptor.script, descriptor.scriptSetup].filter((script): script is NonNullable<typeof script> => script !== null)
    if (scripts.some(script => script.src)) problems.push(`external SFC script requires review: ${relative}`)
    const untyped = scripts.some(script => script.lang !== 'ts')
    if (legacy.has(relative)) {
      remaining.delete(relative)
      if (!untyped) problems.push(`remove migrated SFC from legacy baseline: ${relative}`)
    } else if (untyped) {
      problems.push(`new or previously typed SFC must use inline lang="ts": ${relative}`)
    } else if (scripts.length) typedScripts++
    else templateOnly++
  }
}
inspect(path.join(root, 'src'))
for (const file of remaining) problems.push(`stale legacy baseline entry: ${file}`)
if (problems.length) {
  process.stderr.write(problems.join('\n') + '\n')
  process.exitCode = 1
} else {
  process.stdout.write(`SFC scope: ${components} components; ${typedScripts} TS scripts, ${templateOnly} template-only, ${legacy.size} existing JS scripts tracked for migration\n`)
}
