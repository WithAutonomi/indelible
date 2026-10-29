// Writes the third-party notices for the npm packages that end up in web/dist.
//
// Covers the packages the production build actually bundled (recorded by the
// bundled-packages plugin in vite.config.ts as bundled-packages.json) plus the
// build tools whose generated output is part of the bundle: Tailwind CSS and
// its PrimeUI preset, Vite's runtime helpers and the Vue plugin's component
// helper. Run after `npm run build`; the root notices generator
// (scripts/notices) folds the result into THIRD-PARTY-NOTICES.txt.
//
// Fails when a package ships no licence file, so a dependency change cannot
// leave the notices without a licence text.
//
// Usage: node scripts/third-party-notices.mjs [output-file]
import { existsSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { dirname, extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const webDir = join(dirname(fileURLToPath(import.meta.url)), '..')
const BUILD_OUTPUT_PACKAGES = ['tailwindcss', 'tailwindcss-primeui', 'vite', '@vitejs/plugin-vue']
const LICENCE_PREFIXES = ['LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'UNLICENSE']
const CODE_EXTENSIONS = new Set(['.js', '.mjs', '.cjs', '.ts', '.mts', '.cts', '.json', '.map'])
const RULE = '-'.repeat(80)

const bundledFile = join(webDir, 'bundled-packages.json')
if (!existsSync(bundledFile)) throw new Error('bundled-packages.json is missing; run npm run build first')
const lock = JSON.parse(readFileSync(join(webDir, 'package-lock.json'), 'utf8'))
const paths = [
  ...JSON.parse(readFileSync(bundledFile, 'utf8')),
  ...BUILD_OUTPUT_PACKAGES.map((name) => `node_modules/${name}`),
]

const isLicenceFile = (name) =>
  LICENCE_PREFIXES.some((p) => name.toUpperCase().startsWith(p)) && !CODE_EXTENSIONS.has(extname(name).toLowerCase())

const sections = []
const withoutLicenceFile = []
for (const path of [...new Set(paths)].sort()) {
  const meta = lock.packages?.[path]
  if (!meta) throw new Error(`${path} is not in package-lock.json`)
  const dir = join(webDir, path)
  const files = readdirSync(dir)
    .filter((f) => isLicenceFile(f) && statSync(join(dir, f)).isFile())
    .sort()
  if (files.length === 0) {
    withoutLicenceFile.push(path)
    continue
  }
  const name = path.slice(path.lastIndexOf('node_modules/') + 'node_modules/'.length)
  const lines = [RULE, `${name} ${meta.version}`, `License: ${meta.license ?? 'UNKNOWN'}`, `Source: https://www.npmjs.com/package/${name}/v/${meta.version}`, RULE]
  for (const f of files) {
    const text = readFileSync(join(dir, f), 'utf8').replace(/\r\n/g, '\n').trimEnd()
    lines.push('', `--- ${f} ---`, '', text, '')
  }
  sections.push(lines.join('\n'))
}
if (withoutLicenceFile.length > 0) {
  throw new Error(`no licence file in ${withoutLicenceFile.join(', ')}; add its licence text before shipping`)
}

const header = [
  `The embedded web interface (web/dist) contains code from the ${sections.length} npm packages below:`,
  'the packages bundled by the production build and the build tools whose generated',
  'code or CSS is part of the bundle.',
  '',
].join('\n')
const output = `${header}\n${sections.join('\n')}\n`

const outFile = process.argv[2]
if (outFile) writeFileSync(outFile, output)
else process.stdout.write(output)
