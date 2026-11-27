import { defineConfig, type Plugin } from 'vite'
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { extname, join } from 'node:path'
import { brotliCompress, constants } from 'node:zlib'
import { promisify } from 'node:util'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const compress = promisify(brotliCompress)
const compressible = new Set(['.html', '.js', '.mjs', '.css', '.json', '.svg', '.txt', '.xml', '.webmanifest', '.map'])

function precompress(): Plugin {
  return {
    name: 'yihe-precompress',
    apply: 'build',
    async writeBundle(options) {
      if (!options.dir) return
      for (const file of await readdir(options.dir, { recursive: true, withFileTypes: true })) {
        if (!file.isFile() || !compressible.has(extname(file.name).toLowerCase())) continue
        const path = join(file.parentPath, file.name)
        const raw = await readFile(path)
        if (raw.length < 1024) continue
        const encoded = await compress(raw, {
          params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_MODE]: constants.BROTLI_MODE_TEXT }
        })
        if (encoded.length < raw.length) await writeFile(`${path}.br`, encoded)
      }
    }
  }
}

export default defineConfig({
  plugins: [vue(), precompress()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { proxy: { '/api/v1': 'http://127.0.0.1:8795', '/openapi.json': 'http://127.0.0.1:8795' } },
  build: { target: 'chrome120', sourcemap: false, cssCodeSplit: true, rolldownOptions: { output: { minify: { compress: { dropConsole: true, dropDebugger: true } } } } }
})
