import openapiTS, { astToString } from 'openapi-typescript'

// This build-only workspace owns the generator's supported compiler dependency.
// Application and Vue checks resolve the separate root TypeScript 6 toolchain.
export async function renderOpenApiTypes(document) {
  return astToString(await openapiTS(document))
}
