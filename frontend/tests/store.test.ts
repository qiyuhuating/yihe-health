import { beforeEach, expect, it } from 'vitest'
import store from '@/store'

beforeEach(() => { store.clearTabs() })

it('persists navigation state without exposing resident identity or transient alerts', () => {
  store.addTab({ route: '/operations/overview', name: '运营总览' })
  store.addTab({ route: '/operations/overview', name: '重复' })
  store.activate('/operations/overview')
  store.selectResident(1001)
  expect(store.state.openTab).toHaveLength(1)
  const persisted = JSON.parse(sessionStorage.getItem('yihe-workspace-v1') || '{}')
  expect(store.state.selectedResidentId).toBe('1001')
  expect(persisted).not.toHaveProperty('selectedResidentId')
  expect(persisted.openTab).toEqual([{ route: '/operations/overview', name: '运营总览' }])
  const initial = store.state.alertRevision
  store.notifyAlertsChanged()
  expect(store.state.alertRevision).toBe(initial + 1)
  expect(JSON.parse(sessionStorage.getItem('yihe-workspace-v1') || '{}')).not.toHaveProperty('alertRevision')
  store.clearTabs()
  expect(sessionStorage.getItem('yihe-workspace-v1')).toBeNull()
})

it('limits workspace history without evicting the overview tab', () => {
  store.addTab({ route: '/operations/overview', name: '总览' })
  for (let index = 0; index < 25; index++) {
    store.addTab({ route: `/operations/example-${index}`, name: `页面 ${index}` })
  }
  expect(store.state.openTab).toHaveLength(20)
  expect(store.state.openTab[0]?.route).toBe('/operations/overview')
  expect(store.state.openTab.some(tab => tab.route === '/operations/example-0')).toBe(false)
  expect(store.state.openTab.at(-1)?.route).toBe('/operations/example-24')
  expect(JSON.parse(sessionStorage.getItem('yihe-workspace-v1') || '{}').openTab).toHaveLength(20)
})

it('rejects invalid tabs and retains the first label for duplicate routes', () => {
  store.addTab({ route: '/family-portal/overview', name: '家属首页' })
  store.addTab({ route: '/operations/overview', name: '总览' })
  store.addTab({ route: '/operations/overview', name: '覆盖名称' })
  expect(store.state.openTab).toEqual([{ route: '/operations/overview', name: '总览' }])
})
