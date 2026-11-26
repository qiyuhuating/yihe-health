export interface PositionContext {
  positionSource?: string | null
  positionSampledAt?: string | null
  positionAccuracy?: number | null
  positionValidity?: string | null
  alarmSource?: string | null
  alarmSampledAt?: string | null
  alarmAccuracy?: number | null
  alarmValidity?: string | null
}

const sources: Record<string, string> = {
  gps: 'GPS（设备自报）', network: '网络定位（设备自报）', fused: '融合定位（设备自报）',
  manual: '人工录入', simulated: '模拟定位', unknown: '未知'
}

export function describePositionContext(row: PositionContext, alarm = false, now = Date.now()): string {
  const source = alarm ? row.alarmSource : row.positionSource
  const sampledAt = alarm ? row.alarmSampledAt : row.positionSampledAt
  const accuracy = alarm ? row.alarmAccuracy : row.positionAccuracy
  const validity = alarm ? row.alarmValidity : row.positionValidity
  const sampled = sampledAt ? Date.parse(sampledAt) : NaN
  const sampling = Number.isFinite(sampled)
    ? `${sampledAt}${now - sampled > 5 * 60 * 1000 ? '（历史位置）' : '（设备自报采样时间）'}`
    : '未知（不能判定为实时）'
  const precision = typeof accuracy === 'number' && Number.isFinite(accuracy) && accuracy > 0 ? `${accuracy} 米（设备自报）` : '未知'
  const state = validity === 'reported' ? '设备自报，未经独立核验' : validity === 'missing' ? '本次未提供坐标' : '来源与有效性未知'
  return `来源：${source ? sources[source] || '未知' : '未知'}；采样：${sampling}；精度：${precision}；状态：${state}`
}
