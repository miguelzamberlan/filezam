// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { type Plugin } from 'vite'
// defineConfig vem do vitest: a partir da 4 o tipo do vite não aceita mais a seção 'test'.
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { bundleBanner } from './src/lib/about'

const here = fileURLToPath(new URL('.', import.meta.url))
const outDir = resolve(here, '../internal/server/webdist/dist')
// Fonte única da versão da interface (ver src/lib/about.ts).
const version: string = JSON.parse(readFileSync(resolve(here, 'package.json'), 'utf8')).version

// The Go embed directive needs the dist folder to exist even before a build; keep the placeholder.
const keepFile: Plugin = {
  name: 'filezam-keep',
  closeBundle() {
    mkdirSync(outDir, { recursive: true })
    writeFileSync(resolve(outDir, '.keep'), '')
  },
}

// Aviso legal no topo do JS de entrada. Entra no generateBundle, depois da minificação: o
// output.banner do Rollup passa antes do minificador, que o descartava.
const licenseBanner: Plugin = {
  name: 'filezam-license-banner',
  apply: 'build',
  generateBundle(_, bundle) {
    for (const chunk of Object.values(bundle)) {
      if (chunk.type === 'chunk' && chunk.isEntry) chunk.code = bundleBanner(version) + '\n' + chunk.code
    }
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepFile, licenseBanner],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
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
