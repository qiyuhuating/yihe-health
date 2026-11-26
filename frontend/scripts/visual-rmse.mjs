import { readFileSync } from 'node:fs'
import { PNG } from 'pngjs'

const [baselinePath, actualPath] = process.argv.slice(2)
if (!baselinePath || !actualPath) throw new Error('Expected baseline and actual PNG paths')

const baseline = PNG.sync.read(readFileSync(baselinePath))
const actual = PNG.sync.read(readFileSync(actualPath))
if (baseline.width !== actual.width || baseline.height !== actual.height) {
  throw new Error('Visual regression: screenshot dimensions differ')
}

let squaredError = 0
for (let index = 0; index < baseline.data.length; index += 4) {
  for (let channel = 0; channel < 3; channel++) {
    const difference = baseline.data[index + channel] - actual.data[index + channel]
    squaredError += difference * difference
  }
}
const normalizedRMSE = Math.sqrt(squaredError / (baseline.width * baseline.height * 3 * 255 ** 2))
const limit = 0.08
console.log(`Normalized visual RMSE: ${normalizedRMSE.toFixed(6)}; limit: ${limit.toFixed(6)}`)
if (normalizedRMSE > limit) process.exitCode = 1
