// 最小可离线 Vite 替身——**契约级真调用**，不是真实 Vite。
//
// 它做的不是"读 mappings 再猜一遍别名"，而是加载 `ngm integrations add vite`
// **真实生成的 vite.config.ts**：做一次极小的 TS→CJS 剥离，让 Node 能执行这份
// 配置，拿到它算出的 resolve.alias，再把这些别名写进产物。于是：
//   · 生成器若不再从 mappings 推导 alias（例如改成空数组）→ 这里立刻失败；
//   · 配置文件若不再读 ngm.mappings.json → 前置契约检查立刻失败。
// 放在 cmd/ngm/testdata/ 下，go build / go vet 会跳过；由测试显式调用。
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')

const proj = process.argv[2]
if (!proj) {
  console.error('usage: node vite-substitute.cjs <project-dir>')
  process.exit(3)
}

const configPath = path.join(proj, 'vite.config.ts')
const mappingsPath = path.join(proj, 'ngm.mappings.json')
const outfile = path.join(proj, 'dist', 'index.js')

if (!fs.existsSync(configPath) || !fs.existsSync(mappingsPath)) {
  console.error('missing generated config or mappings file')
  process.exit(2)
}

const src = fs.readFileSync(configPath, 'utf8')
for (const marker of ['ngm.mappings.json', 'resolve', 'alias', 'specifier', 'replacement']) {
  if (!src.includes(marker)) {
    console.error('generated vite.config.ts is missing contract marker: ' + marker)
    process.exit(2)
  }
}

// 极小 TS 剥离：只针对生成器产出的固定形状；形状漂移应当让判据变红。
const js =
  src
    .replace("import { defineConfig } from 'vite'", 'const defineConfig = (x) => x')
    .replace(
      "import mappings from './ngm.mappings.json'",
      "const mappings = JSON.parse(require('node:fs').readFileSync(" +
        JSON.stringify(mappingsPath) +
        ", 'utf8'))",
    )
    .replace(/^type Mapping = .*$/m, '')
    .replace(/\(m: Mapping\)/g, '(m)')
    .replace('(mappings.mappings as Mapping[])', 'mappings.mappings')
    .replace('export default defineConfig({', 'const __config = defineConfig({') +
  '\nmodule.exports = __config\n'

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ngm-vite-sub-'))
const cjsPath = path.join(tmp, 'config.cjs')
try {
  fs.writeFileSync(cjsPath, js, 'utf8')
  const config = require(cjsPath)
  const aliases = config && config.resolve && config.resolve.alias
  if (!Array.isArray(aliases) || aliases.length === 0) {
    console.error('resolve.alias is empty - mappings were not consumed')
    process.exit(2)
  }
  for (const a of aliases) {
    if (!a || typeof a.find !== 'string' || typeof a.replacement !== 'string' || !a.find) {
      console.error('malformed alias entry: ' + JSON.stringify(a))
      process.exit(2)
    }
  }
  const lines = aliases.map((a) => '  ' + a.find + ' -> ' + a.replacement)
  const out = '/* offline vite substitute bundle */\n' + lines.join('\n') + '\n'
  fs.mkdirSync(path.dirname(outfile), { recursive: true })
  fs.writeFileSync(outfile, out, 'utf8')
  console.log(
    'vite-substitute (contract-level, NOT real Vite): ' +
      aliases.length +
      ' alias(es) -> dist/index.js',
  )
} finally {
  fs.rmSync(tmp, { recursive: true, force: true })
}
