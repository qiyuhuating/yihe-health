import { accepted } from './client'
import { postContract, type JsonReply } from './contractClient'

export type LatestHealthRecord = JsonReply<'/api/v1/health/latest'>['items'][number]

export async function readLatestHealth(signal: AbortSignal): Promise<LatestHealthRecord[]> {
  const { data } = await postContract('/api/v1/health/latest', {}, { signal })
  accepted(data)
  return data.items
}
