import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { proxy: { '/api/v1': 'http://127.0.0.1:8795', '/openapi.json': 'http://127.0.0.1:8795' } },
  build: { target: 'chrome120', sourcemap: false, cssCodeSplit: true, rolldownOptions: { output: { minify: { compress: { dropConsole: true, dropDebugger: true } } } } }
})
