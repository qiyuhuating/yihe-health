import path from 'node:path'
import { cpSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { exportDocument, generateTypes } from './api-contract.mjs'

const root = fileURLToPath(new URL('..', import.meta.url))
// Keep the fixture outside src/tests, but beneath the project so the real
// consumer resolves the installed dependencies without links or source edits.
const temporary = mkdtempSync(path.join(root, '.api-drift-'))
const fixturePath = path.join(temporary, 'src/api/__contract_drift_probe__.ts')
const compiler = path.join(root, 'node_modules/typescript/bin/tsc')
const fixture = `
import { postContract, type JsonReply } from './contractClient'
postContract('/api/v1/health/latest', { oldId: 1 })
declare const reply: JsonReply<'/api/v1/health/latest'>
const metric: string = reply.items[0].sample.metric
const device: number | null = reply.items[0].deviceId
const timestamp: string | undefined = reply.items[0].sample.metadata?.timestampBasis
`

function compile(generated) {
  writeFileSync(path.join(temporary, 'src/api/generated-api.d.ts'), generated)
  const result = spawnSync(process.execPath, [compiler, '--noEmit', '--pretty', 'false', '-p', temporary], {
    cwd: temporary, encoding: 'utf8', windowsHide: true, timeout: 60000, maxBuffer: 8 * 1024 * 1024
  })
  if (result.error || result.signal || result.status === null) throw result.error || new Error('Contract compiler did not complete')
  return { status: result.status, output: result.stdout + result.stderr }
}

try {
  cpSync(path.join(root, 'src'), path.join(temporary, 'src'), { recursive: true })
  writeFileSync(fixturePath, fixture)
  writeFileSync(path.join(temporary, 'tsconfig.json'), JSON.stringify({
    compilerOptions: {
      strict: true, noEmit: true, skipLibCheck: true, types: [], rootDir: './src',
      target: 'ES2022', module: 'ESNext', moduleResolution: 'Bundler', esModuleInterop: true
    },
    files: ['src/api/__contract_drift_probe__.ts']
  }))
  const original = exportDocument()
  const baseline = compile(await generateTypes(original))
  if (baseline.status !== 0) {
    console.error(baseline.output)
    throw new Error('Current generated contract must compile before testing drift')
  }
  console.log('Current Go contract and typed consumer compile')
  const latest = document => document.paths['/api/v1/health/latest'].post
  for (const [name, mutate] of [
    ['removed route', document => {
      document.paths['/api/v1/health/latest-renamed'] = document.paths['/api/v1/health/latest']
      delete document.paths['/api/v1/health/latest']
    }],
    ['changed request field type', document => {
      latest(document).requestBody.content['application/json'].schema.properties.oldId.type = 'string'
    }],
    ['renamed required response field', document => {
      const sample = latest(document).responses['200'].content['application/json'].schema.properties.items.items.properties.sample
      sample.properties.metricName = sample.properties.metric
      delete sample.properties.metric
      sample.required = sample.required.map(name => name === 'metric' ? 'metricName' : name)
    }],
    ['changed metadata value type', document => {
      const sample = latest(document).responses['200'].content['application/json'].schema.properties.items.items.properties.sample
      sample.properties.metadata.additionalProperties.type = 'integer'
    }]
  ]) {
    const changed = structuredClone(original)
    mutate(changed)
    const result = compile(await generateTypes(changed))
    const errors = result.output.split(/\r?\n/).filter(line => /error TS\d+:/.test(line))
    const consumerRejected = errors.some(line => /__contract_drift_probe__\.ts\(\d+,\d+\): error TS(?:2322|2339|2344|2345):/.test(line))
    if (result.status === 0 || !consumerRejected || errors.some(line => !/error TS(?:2322|2339|2344|2345):/.test(line))) {
      console.error(result.output)
      throw new Error(`Typed consumer did not reject API drift: ${name}`)
    }
    console.log(`Expected compile rejection: ${name}`)
  }
} finally {
  if (path.dirname(temporary) !== path.resolve(root) || !path.basename(temporary).startsWith('.api-drift-')) throw new Error('Unsafe contract fixture path')
  rmSync(temporary, { recursive: true, force: true })
}
