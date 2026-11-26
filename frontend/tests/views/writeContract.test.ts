import { expect, it } from 'vitest'
import { editableResident, residentFields } from '@/composables/residentFields'

it('serializes only editable resident fields without old display metadata', () => {
  const editable = {
    name: '长者', bothdate: '1950-01-01', sex: '女',
    phone: '13800000001', room: 'A', illness: '', warn: '',
    foodLike: '', foodHate: '', drag: '', customerId: 2
  }
  const draft = {
    ...editable, oldId: 7, beTime: 'old', familyName: 'display',
    deviceId: 3, customerName: 'display'
  }
  expect(Object.keys(editableResident(draft))).toEqual([...residentFields])
  expect(editableResident(draft)).toEqual(editable)
})
