import { reactive } from 'vue'

export interface Tab { route: string; name: string }
interface WorkspaceState {
  openTab: Tab[]
  activeIndex: string
  selectedResidentId: string | null
  alertRevision: number
}
const storageKey = 'yihe-workspace-v1'
const home = '/operations/overview'
const maximumTabs = 20

function validTab(value: unknown): value is Tab {
  if (!value || typeof value !== 'object') return false
  const tab = value as Partial<Tab>
  return typeof tab.route === 'string' && tab.route.startsWith('/operations/') &&
    typeof tab.name === 'string' && tab.name.length > 0
}

function restoreTabs(value: unknown): Tab[] {
  if (!Array.isArray(value)) return []
  const routes = new Set<string>()
  const tabs: Tab[] = []
  for (const item of value) {
    if (!validTab(item) || routes.has(item.route)) continue
    tabs.push({ route: item.route, name: item.name })
    routes.add(item.route)
    if (tabs.length === maximumTabs) break
  }
  return tabs
}

function restoreWorkspace(): Pick<WorkspaceState, 'openTab' | 'activeIndex'> {
  try {
    const raw = sessionStorage.getItem(storageKey)
    if (raw) {
      const saved = JSON.parse(raw) as Partial<WorkspaceState>
      return {
        openTab: restoreTabs(saved.openTab),
        activeIndex: typeof saved.activeIndex === 'string' && saved.activeIndex.startsWith('/operations/')
          ? saved.activeIndex : home
      }
    }
  } catch { /* Storage-disabled sessions keep the workspace in memory. */ }
  return { openTab: [], activeIndex: home }
}
const state = reactive<WorkspaceState>({ ...restoreWorkspace(), selectedResidentId: null, alertRevision: 0 })
function persistWorkspace() {
  try {
    sessionStorage.setItem(storageKey, JSON.stringify({
      openTab: state.openTab, activeIndex: state.activeIndex
    }))
  } catch { /* Storage availability must not break navigation. */ }
}
// Scrub identifiers persisted by earlier sessions; only navigation state survives reload.
persistWorkspace()
const alertsChannel = typeof window !== 'undefined' && typeof window.BroadcastChannel === 'function'
  ? new window.BroadcastChannel('yihe-alerts-v1') : null
if (alertsChannel) alertsChannel.onmessage = event => {
  if (event.data && typeof event.data === 'object' && (event.data as { type?: unknown }).type === 'alerts-changed') state.alertRevision++
}

const store = {
  state,
  addTab(tab: Tab) {
    if (!validTab(tab) || state.openTab.some(item => item.route === tab.route)) return
    if (state.openTab.length >= maximumTabs) {
      const oldestClosable = state.openTab.findIndex(item => item.route !== home)
      if (oldestClosable === -1) return
      state.openTab.splice(oldestClosable, 1)
    }
    state.openTab.push(tab)
    persistWorkspace()
  },
  removeTab(route: string) {
    state.openTab = state.openTab.filter(tab => tab.route !== route)
    persistWorkspace()
  },
  clearTabs() {
    state.openTab = []
    state.activeIndex = home
    state.selectedResidentId = null
    try { sessionStorage.removeItem(storageKey) } catch { /* Storage may be unavailable. */ }
  },
  activate(route: string) {
    state.activeIndex = route
    persistWorkspace()
  },
  selectResident(id: string | number | null) {
    state.selectedResidentId = id == null ? null : String(id)
    persistWorkspace()
  },
  notifyAlertsChanged() {
    state.alertRevision++
    alertsChannel?.postMessage({ type: 'alerts-changed' })
  }
}
export default store
