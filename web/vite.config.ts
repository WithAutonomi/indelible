import { writeFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// Records which npm packages end up in the production bundle, so that
// scripts/third-party-notices.mjs reproduces the licences of exactly those.
function bundledPackages(): Plugin {
  let root = ''
  return {
    name: 'indelible:bundled-packages',
    apply: 'build',
    configResolved(config) {
      root = config.root
    },
    generateBundle(_options, bundle) {
      const packages = new Set<string>()
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        for (const id of output.moduleIds) {
          const file = id.replace(/^\0/, '').split('?')[0].replace(/\\/g, '/')
          const dir = file.match(/^.*\/node_modules\/(?:@[^/]+\/)?[^/]+/)?.[0]
          if (dir) packages.add(relative(root, dir).replace(/\\/g, '/'))
        }
      }
      writeFileSync(join(root, 'bundled-packages.json'), `${JSON.stringify(Array.from(packages).sort(), null, 2)}\n`)
    },
  }
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), bundledPackages()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
