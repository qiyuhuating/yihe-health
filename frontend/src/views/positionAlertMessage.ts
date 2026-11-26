import { describePositionContext, type PositionContext } from '../utils/positionContext'

export interface PositionAlarm extends PositionContext {
  name?: string
  alarmTime?: string | null
  alarmLng?: string | number | null
  alarmLat?: string | number | null
  alarmKind?: string
  [key: string]: unknown
}

export function positionAlertMessage(row: PositionAlarm): { message: string; title: string } {
  const coordinates = row.alarmLng != null && row.alarmLat != null &&
    String(row.alarmLng).trim() !== '' && String(row.alarmLat).trim() !== ''
  const position = coordinates
    ? '经度 ' + row.alarmLng + '，纬度 ' + row.alarmLat
    : '未提供告警坐标（最新定位仅供参考）'
  const kind = ({ sos: 'SOS 求助', fall: '设备上报摔倒' } as Record<string,string>)[row.alarmKind || ''] || '未知'
  const message = [
    '老人：' + (row.name || '未知'),
    '告警时间：' + (row.alarmTime || '未提供告警时间'),
    '告警附带坐标：' + position,
    '事件类型：' + kind,
    describePositionContext(row, true), '',
    '怎么处理：',
    '① 立即电话联系老人或家属确认情况；',
    '② 结合告警附带坐标核实位置，必要时请求现场协助；',
    '③ 确认处理后点「标记已处理」，右上角告警红点数量会减少。'
  ].join('\n')
  return { message, title: '紧急告警 · 原因与处理' }
}
