import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { writeFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const outDir = resolve(fileURLToPath(new URL('.', import.meta.url)), '../internal/server/webdist/dist')

// The Go embed directive needs the dist folder to exist even before a build; keep the placeholder.
const keepFile: Plugin = {
  name: 'filezam-keep',
  closeBundle() {
    mkdirSync(outDir, { recursive: true })
    writeFileSync(resolve(outDir, '.keep'), '')
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepFile],
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
