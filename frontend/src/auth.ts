import { reactive } from 'vue'
import axios, { type AxiosError } from 'axios'
import { REQUEST_TIMEOUT_MS } from './api/client'
import { ElMessage } from 'element-plus'
import { apiErrorMessage, assertApiResponse, isApplicationAPI } from './api/httpContract'
import store from './store'
import { isRestrictedAdmin } from './domain/permissions'
import type { SessionAccount } from './domain/session'
export type { SessionAccount } from './domain/session'

interface SessionWire {
  csrfToken: string
  csrfHeader?: string
  setupRequired?: boolean
  authenticated?: boolean
  account?: SessionAccount | null
}

export interface SessionState {
  account: SessionAccount | null
  csrfToken: string
  csrfHeader: string
  setupRequired: boolean
  databaseReady: boolean | null
  configError: string
}
declare module 'axios' {
  interface AxiosRequestConfig { skipSession?: boolean }
}
export const authState = reactive<SessionState>({
  account: null, csrfToken: '', csrfHeader: 'X-CSRF-TOKEN',
  setupRequired: false, databaseReady: null, configError: ''
})
let loaded = false
let pending: Promise<SessionAccount | null> | null = null
let sessionGeneration = 0
const profileKeys = ['token', 'user_name', 'oldIdfamily', 'familyPhone', 'familyTouPic']

function clearProfile () {
  for (const key of profileKeys) {
    try { localStorage.removeItem(key) } catch { /* Optional browser storage. */ }
  }
}

function invalidateSession(clearIdentity = true) {
  sessionGeneration++
  pending = null
  loaded = false
  authState.csrfToken = ''
  if (!clearIdentity) return
  authState.account = null
  authState.csrfHeader = 'X-CSRF-TOKEN'
  clearProfile()
  store.clearTabs()
}

export function loadSession (refresh = false): Promise<SessionAccount | null> {
  if (!refresh && loaded) return Promise.resolve(authState.account)
  if (pending) return pending
  const generation = sessionGeneration
  const request = axios.get<SessionWire>('/api/v1/auth/session', { skipSession: true, timeout: 5000 }).then(response => {
    if (generation !== sessionGeneration) return authState.account
    const data = response.data
    if (!data || typeof data.csrfToken !== 'string' || !data.csrfToken ||
      typeof data.authenticated !== 'boolean' ||
      (data.setupRequired !== undefined && typeof data.setupRequired !== 'boolean') ||
      (data.csrfHeader !== undefined && typeof data.csrfHeader !== 'string') ||
      (data.authenticated && (!data.account || !['ADMIN', 'FAMILY'].includes(data.account.role)))) {
      throw new Error('Invalid session response')
    }
    authState.setupRequired = Boolean(data.setupRequired)
    const previousAccount = authState.account
    const nextAccount = data.authenticated ? data.account as SessionAccount : null
    authState.account = nextAccount
    authState.csrfToken = data.csrfToken
    authState.csrfHeader = data.csrfHeader || 'X-CSRF-TOKEN'
    loaded = true
    clearProfile()
    const identityChanged = previousAccount !== null && nextAccount !== null && (
      String(previousAccount.id ?? '') !== String(nextAccount.id ?? '') ||
      previousAccount.role !== nextAccount.role ||
      previousAccount.phone !== nextAccount.phone ||
      String(previousAccount.oldId ?? '') !== String(nextAccount.oldId ?? '') ||
      previousAccount.duty !== nextAccount.duty
    )
    if (!nextAccount || identityChanged) store.clearTabs()
    return authState.account
  })
  const flight = request.finally(() => { if (pending === flight) pending = null })
  pending = flight
  return flight
}

export async function endSession () {
  try {
    await axios.post('/api/v1/auth/logout', {}, { skipSession: true, timeout: REQUEST_TIMEOUT_MS, headers: { [authState.csrfHeader]: authState.csrfToken } })
  } finally {
    // A lost response may still mean the server destroyed the session. Keeping
    // stale credentials would show an account that no longer exists until the
    // next request happens to return 401.
    invalidateSession()
  }
}

export function installHttpSecurity (router: { currentRoute: { value: { path: string } }; replace?: (path: string) => unknown }) {
  axios.defaults.withCredentials = true
  // Covers the calls that bypass api.post (local logout, session probe).
  axios.defaults.timeout = REQUEST_TIMEOUT_MS
  axios.interceptors.request.use(config => {
    if (config.skipSession || !isApplicationAPI(config.url)) return config
    if (['get', 'head', 'options'].includes((config.method || 'get').toLowerCase())) return config
    return loadSession().then(() => {
      config.headers[authState.csrfHeader] = authState.csrfToken
      return config
    })
  })
  axios.interceptors.response.use(assertApiResponse, (error: AxiosError<{ code?: string }>) => {
    error.message = apiErrorMessage(error, error.message || '连接中断')
    if (error.response?.data?.code === 'DATABASE_UNAVAILABLE') {
      loaded = false
      authState.databaseReady = false
      if (!error.config?.skipSession) ElMessage.warning({ message: authState.configError
        ? '服务端配置有误，依赖数据库的功能不可用，请联系管理员修正后重启。'
        : '此功能需要连接数据库，正在自动连接。恢复后请重试。', grouping: true })
    }
    if (error.response && error.response.status === 401) {
      invalidateSession()
      if (!['/sign-in', '/join-family'].includes(router.currentRoute.value.path)) router.replace?.('/sign-in')
    }
    if (error.response && error.response.status === 403 && error.response.data && error.response.data.code === 'CSRF_REJECTED') {
      invalidateSession(false)
      // Refresh the token without replaying the rejected write.
      loadSession(true).catch(() => {})
    }
    if (error.response?.status === 403 && error.response.data?.code === 'ACCESS_DENIED' && authState.account?.role === 'ADMIN') {
      // Refresh changed permissions without replaying the rejected write.
      loadSession(true).then(account => {
        if (isRestrictedAdmin(account) && !['/operations/overview', '/operations'].includes(router.currentRoute.value.path)) {
          router.replace?.('/operations/overview')
        }
      }).catch(() => {})
    }
    return Promise.reject(error)
  })
}


// Probe readiness with backoff; never retry writes implicitly.
export function monitorDatabase (router: { currentRoute: { value: { path: string } }; replace?: (path: string) => unknown }) {
  let active = true
  let checking = false
  let failures = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  async function check () {
    if (!active || checking || document.hidden) return
    clearTimeout(timer)
    checking = true
    const previous = authState.databaseReady
    let ready = false
    try {
      const { data } = await axios.get<{ databaseReady: boolean; configError?: string }>('/api/v1/service/status', { skipSession: true, timeout: 5000 })
      if (!active) return
      if (typeof data.databaseReady !== 'boolean') throw new Error('Invalid database status')
      authState.databaseReady = data.databaseReady
      authState.configError = typeof data.configError === 'string' ? data.configError : ''
      ready = data.databaseReady
      if (!ready) loaded = false
      if (ready && previous === false) {
        const account = await loadSession(true)
        if (!active) return
        const route = router.currentRoute.value.path
        if (authState.setupRequired && route === '/sign-in') await router.replace?.('/setup')
        else if (account && route === '/sign-in') await router.replace?.(account.role === 'FAMILY' ? '/family-portal/overview' : '/operations/overview')
        else if (!account && !['/sign-in', '/join-family', '/setup'].includes(route)) await router.replace?.('/sign-in')
      }
    } catch {
      ready = false
      if (active) { authState.databaseReady = false; loaded = false }
    } finally {
      checking = false
      if (active && !document.hidden) {
        failures = ready ? 0 : Math.min(failures + 1, 5)
        const delay = ready ? 10000 : Math.min(20000, 2000 * 2 ** (failures - 1))
        timer = setTimeout(check, delay)
      }
    }
  }
  function resume () {
    if (!active || document.hidden || checking) return
    clearTimeout(timer)
    void check()
  }
  function onVisibilityChange () {
    if (document.hidden) clearTimeout(timer)
    else resume()
  }

  window.addEventListener('online', resume)
  document.addEventListener('visibilitychange', onVisibilityChange)
  resume()
  return () => {
    active = false
    clearTimeout(timer)
    window.removeEventListener('online', resume)
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }
}
