import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { inlineScripts } from './sfc-scripts.ts'

const sourceRoot = new URL('../src/', import.meta.url)
interface PackageInfo { dependencies?: Record<string, string>; devDependencies?: Record<string, string>; optionalDependencies?: Record<string, string> }
const packageInfo = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')) as PackageInfo
const forbiddenPackages = [
  'vue-template-compiler', '@vue/compat', 'vuex', 'vue-class-component',
  'vue-property-decorator', 'vue-router-composable', 'element-ui',
  'vue-cli-service', '@vue/cli-service', 'vue-loader', 'vue-jest'
]
type PatternRule = readonly [RegExp, string]
const forbiddenSyntax: PatternRule[] = [
  [/\bnew\s+Vue\s*\(/g, 'Vue 2 constructor'],
  [/\bVue\.(?:use|extend|component|directive|filter|mixin|prototype)\b/g, 'Vue 2 global API'],
  [/\b(?:this\.)?\$(?:listeners|scopedSlots|children|destroy|set|delete|on|off|once)\b/g, 'removed instance API'],
  [/\b(?:beforeDestroy|destroyed)\s*\(/g, 'removed lifecycle hook']
]
const forbiddenTemplate: PatternRule[] = [
  [/\bv-bind:[\w-]+\.sync\b|\s:[\w-]+\.sync\b/g, 'removed .sync binding'],
  [/@[\w:-]+\.native\b/g, 'removed .native modifier'],
  [/\bslot-scope\s*=/g, 'removed scoped-slot syntax']
]

const errors: string[] = []
const advisory: string[] = []
const sourcePath = fileURLToPath(sourceRoot)
for (const section of ['dependencies', 'devDependencies', 'optionalDependencies'] as const) {
  for (const name of Object.keys(packageInfo[section] ?? {})) {
    if (forbiddenPackages.includes(name) || name.startsWith('@vue/cli-plugin-')) {
      errors.push(`package.json: ${section} contains ${name}`)
    }
  }
}
if (!String(packageInfo.dependencies?.vue ?? '').startsWith('3.')) {
  errors.push('package.json: vue must remain on major version 3')
}

function walk(directory: string): void {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      walk(path)
    } else if (/\.(?:vue|ts|tsx|js|jsx)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
      check(path)
    }
  }
}

let scanned = 0
let jsSfcs = 0
function check(path: string): void {
  scanned++
  const text = readFileSync(path, 'utf8')
  const name = relative(sourcePath, path)
  const isSfc = path.endsWith('.vue')
  const blocks = isSfc ? inlineScripts(text, path) : []
  const script = isSfc
    ? blocks.map(block => block.content).join('\n')
    : text
  if (isSfc) {
    if (blocks.some(block => block.lang !== 'ts')) jsSfcs++
    for (const [pattern, reason] of forbiddenTemplate) {
      for (const match of text.matchAll(pattern)) {
        const line = text.slice(0, match.index).split('\n').length
        errors.push(`${name}:${line}: ${reason} (${match[0]})`)
      }
    }
  }
  for (const [pattern, reason] of forbiddenSyntax) {
    for (const match of script.matchAll(pattern)) {
      const line = script.slice(0, match.index).split('\n').length
      errors.push(`${name}:${line}: ${reason} (${match[0]})`)
    }
  }
}
walk(sourcePath)
advisory.push(`${jsSfcs} JavaScript SFCs need incremental migration to <script setup lang="ts"> before Vapor eligibility`)
console.log(`Vue 3.6 readiness: scanned ${scanned} source files; ${advisory.join('; ')}`)
if (errors.length) {
  console.error(errors.join('\n'))
  process.exitCode = 1
}
