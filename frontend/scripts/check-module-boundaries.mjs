import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'
import ts from 'typescript'
import { inlineScripts } from './sfc-scripts.ts'

const root = resolve('src')
const sourceExtensions = ['.ts', '.tsx', '.vue', '.js', '.mjs']
const walk = directory => readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
  const file = join(directory, entry.name)
  return entry.isDirectory() ? walk(file) : sourceExtensions.some(ext => file.endsWith(ext)) ? [file] : []
})
const files = walk(root)
const known = new Set(files)
const edges = new Map(files.map(file => [file, new Set()]))
const violations = []

const allowed = {
  domain: new Set(['domain']),
  api: new Set(['api', 'domain']),
  utils: new Set(['utils', 'domain']),
  charts: new Set(['charts', 'utils', 'domain']),
  composables: new Set(['composables', 'api', 'domain', 'utils', 'charts']),
  components: new Set(['components', 'composables', 'api', 'domain', 'utils', 'root'])
}
const pathOf = file => relative(root, file).split(sep).join('/')
const layer = file => pathOf(file).split('/').length === 1 ? 'root' : pathOf(file).split('/')[0]

function targetOf(file, specifier) {
  if (!specifier.startsWith('./') && !specifier.startsWith('../') && !specifier.startsWith('@/')) return null
  if (/\.(css|scss|svg|png|webp|json)(?:\?|$)/.test(specifier)) return null
  const path = specifier.startsWith('@/')
    ? resolve(root, specifier.slice(2))
    : resolve(dirname(file), specifier)
  const candidates = [path, ...sourceExtensions.map(ext => path + ext), path + '.d.ts',
    ...sourceExtensions.map(ext => join(path, 'index' + ext))]
  const found = candidates.find(candidate => known.has(candidate))
  if (!found) violations.push(pathOf(file) + ': unresolved local import ' + specifier)
  return found || null
}

for (const file of files) {
  const source = readFileSync(file, 'utf8')
  const scripts = file.endsWith('.vue')
    ? inlineScripts(source, file).map(block => block.content)
    : [source]
  for (const script of scripts) {
    const tree = ts.createSourceFile(file, script, ts.ScriptTarget.Latest, true)
    const visit = node => {
      let specifier
      if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
        if (node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
          specifier = node.moduleSpecifier.text
        }
      } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword &&
        node.arguments.length === 1 && ts.isStringLiteral(node.arguments[0])) {
        specifier = node.arguments[0].text
      }
      if (specifier) {
        const target = targetOf(file, specifier)
        if (target) {
          edges.get(file).add(target)
          const layers = allowed[layer(file)]
          if (layers && !layers.has(layer(target))) {
            violations.push(pathOf(file) + ' -> ' + pathOf(target) + ': ' + layer(file) + ' depends on ' + layer(target))
          }
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(tree)
  }
}

// Reject both runtime and type-only cycles, since the latter couple module ownership.
const ids = new Map(), lows = new Map(), active = new Set(), stack = []
let next = 0
function visit(file) {
  ids.set(file, next)
  lows.set(file, next++)
  stack.push(file)
  active.add(file)
  for (const dependency of edges.get(file)) {
    if (!ids.has(dependency)) {
      visit(dependency)
      lows.set(file, Math.min(lows.get(file), lows.get(dependency)))
    } else if (active.has(dependency)) {
      lows.set(file, Math.min(lows.get(file), ids.get(dependency)))
    }
  }
  if (lows.get(file) !== ids.get(file)) return
  const group = []
  let member
  do {
    member = stack.pop()
    active.delete(member)
    group.push(member)
  } while (member !== file)
  if (group.length > 1 || edges.get(file).has(file)) {
    violations.push('import cycle: ' + group.map(pathOf).join(' <-> '))
  }
}
for (const file of files) if (!ids.has(file)) visit(file)

if (violations.length) {
  for (const violation of violations) console.error(violation)
  process.exitCode = 1
} else {
  console.log('Module boundaries OK: ' + files.length + ' files, ' +
    [...edges.values()].reduce((n, e) => n + e.size, 0) + ' internal imports')
}
