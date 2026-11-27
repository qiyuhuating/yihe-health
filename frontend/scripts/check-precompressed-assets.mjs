import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { brotliDecompressSync } from 'node:zlib'

const files = await readdir('dist', { recursive: true, withFileTypes: true })
const assets = files.filter(file => file.isFile() && file.name.endsWith('.br'))
assert(assets.some(file => file.name.endsWith('.js.br')), 'Missing precompressed JavaScript')
for (const file of assets) {
  const path = join(file.parentPath, file.name)
  const encoded = await readFile(path)
  const raw = await readFile(path.slice(0, -3))
  assert(encoded.length < raw.length, `Precompression did not reduce ${path}`)
  assert(brotliDecompressSync(encoded).equals(raw), `Brotli content mismatch: ${path}`)
}
console.log(`Verified ${assets.length} precompressed assets`)
