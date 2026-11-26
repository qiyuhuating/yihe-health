import type { paths } from './generated-api'
import { api, type PostOptions } from './client'

export type JsonPostPath = {
  [Path in keyof paths]: paths[Path]['post'] extends {
    requestBody: { content: { 'application/json': unknown } }
  } ? Path : never
}[keyof paths]

type Operation<Path extends JsonPostPath> = NonNullable<paths[Path]['post']>
export type JsonRequest<Path extends JsonPostPath> = Operation<Path> extends {
  requestBody: { content: { 'application/json': infer Body } }
} ? Body : never
export type JsonReply<Path extends JsonPostPath> = Operation<Path> extends {
  responses: { 200: { content: { 'application/json': infer Body } } }
} ? Body : never

// Keep the existing Axios instance, cookie/CSRF handling and cancellation. The
// generated request/response types do not replace runtime response validation.
export function postContract<Path extends JsonPostPath>(path: Path, body: JsonRequest<Path>, options?: PostOptions) {
  return api.post<JsonReply<Path>>(path.substring(1), body, options)
}
