import fs from 'node:fs'
import path from 'node:path'

function findSourceRoot(): string {
  let directory = process.cwd()
  for (let depth = 0; depth < 5; depth++) {
    const candidate = path.join(directory, 'src')
    if (fs.existsSync(path.join(candidate, 'api', 'client.ts'))) return candidate
    directory = path.dirname(directory)
  }
  throw new Error('frontend src directory not found')
}
export const SRC = findSourceRoot()

export function sourceFiles(directory: string): string[] {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const full = path.join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(full)
    return /\.(?:ts|vue)$/.test(entry.name) ? [full] : []
  })
}
