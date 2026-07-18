import { copyFileSync, cpSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const assetRoot = resolve(frontendRoot, 'public/markdown-assets')
const katexRoot = resolve(frontendRoot, 'node_modules/katex/dist')
const katexAssetRoot = resolve(assetRoot, 'katex')

mkdirSync(assetRoot, { recursive: true })
mkdirSync(katexAssetRoot, { recursive: true })
copyFileSync(
  resolve(frontendRoot, 'node_modules/mermaid/dist/mermaid.min.js'),
  resolve(assetRoot, 'mermaid.min.js'),
)
copyFileSync(resolve(katexRoot, 'katex.min.css'), resolve(katexAssetRoot, 'katex.min.css'))
copyFileSync(resolve(katexRoot, 'katex.min.js'), resolve(katexAssetRoot, 'katex.min.js'))
copyFileSync(
  resolve(katexRoot, 'contrib/auto-render.min.js'),
  resolve(katexAssetRoot, 'auto-render.min.js'),
)
cpSync(resolve(katexRoot, 'fonts'), resolve(katexAssetRoot, 'fonts'), { recursive: true })
