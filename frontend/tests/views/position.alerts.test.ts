import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RecordCards from '@/components/RecordCards.vue'
import { describePositionContext } from '@/utils/positionContext'
import { positionAlertMessage } from '@/views/positionAlertMessage'

it('marks historical sampling and keeps unprovided context unknown', () => {
  const now = Date.parse('2026-10-31T10:00:00Z')
  const unknown = describePositionContext({}, false, now)
  expect(unknown).toContain('不能判定为实时')
  expect(unknown).not.toContain('GPS')
  const historical = describePositionContext({
    alarmSource: 'gps', alarmSampledAt: '2026-10-20T02:00:00Z',
    alarmAccuracy: 12.5, alarmValidity: 'reported'
  }, true, now)
  expect(historical).toContain('历史位置')
  expect(historical).toContain('12.5 米')
  expect(historical).toContain('未经独立核验')
})

it('uses the SOS incident snapshot instead of the later location', () => {
  const { message, title } = positionAlertMessage({
    name: '长者', alarmKind: 'sos', lng: '121', lat: '31',
    beTime: '2026-10-20 11:00:00', alarmTime: '2026-10-20 10:00:00',
    alarmLng: '120', alarmLat: '30'
  })
  expect(title).toBe('紧急告警 · 原因与处理')
  expect(message).toContain('事件类型：SOS 求助')
  expect(message).toContain('2026-10-20 10:00:00')
  expect(message).toContain('经度 120，纬度 30')
  expect(message).not.toContain('2026-10-20 11:00:00')
  expect(message).not.toContain('经度 121')
  expect(message).not.toContain('设备上报摔倒')
})

it('reports unknown incident coordinates but accepts explicit zero', () => {
  const missing = positionAlertMessage({ lng: '121', lat: '31', alarmLng: null, alarmLat: null }).message
  expect(missing).toContain('未提供告警坐标')
  expect(missing).not.toContain('经度 121')
  expect(positionAlertMessage({ alarmLng: '0', alarmLat: '0' }).message).toContain('经度 0，纬度 0')
})

it('does not classify the shared emergency status as a confirmed fall', async () => {
  const view = mount(RecordCards, { props: {
    records: [{ oldId: 1, name: '长者', status: '1' }],
    fields: [{ key: 'status', label: '告警状态', state: 'fall' }],
    label: '位置记录'
  } })
  expect(view.text()).toContain('紧急告警')
  expect(view.text()).not.toContain('摔倒')
  await view.setProps({ records: [{ oldId: 1, name: '长者', status: '0' }] })
  expect(view.text()).toContain('未告警/已处理')
  view.unmount()
})
