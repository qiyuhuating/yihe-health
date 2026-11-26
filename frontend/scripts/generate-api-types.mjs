import { generate } from './api-contract.mjs'

await generate(process.argv.includes('--check'))
