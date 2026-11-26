import type { Component } from 'vue'
import { createRouter, createWebHashHistory, type RouteLocationNormalized, type RouteRecordRaw } from 'vue-router'
import { loadSession, authState, type SessionAccount } from './auth'
import { documentTitle, recoverFromChunkError } from './appGuards'
import { isRestrictedAdmin } from './domain/permissions'

const NoticeEditor = () => import('./components/NoticeEditor.vue')
type Page = [path: string, title: string, component: () => Promise<{ default: Component }>, props?: Record<string, boolean>]
const page = ([path, title, component, props]: Page): RouteRecordRaw => ({
  path, name: path.slice(1).replaceAll('/', '-'), meta: { title }, component, props
})

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/sign-in' },
    { path: '/setup', component: () => import('./views/Setup.vue') },
    { path: '/sign-in', component: () => import('./views/Login.vue') },
    { path: '/join-family', component: () => import('./views/Register.vue') },
    {
      path: '/family-portal',
      component: () => import('./views/FamilyPortal.vue'),
      redirect: '/family-portal/overview',
      children: ([
        ['/family-portal/overview', '家属首页', () => import('./views/FamilyOverview.vue')],
        ['/family-portal/vitals', '心率血压', () => import('./views/FamilyVitals.vue')],
        ['/family-portal/activity', '睡眠步数', () => import('./views/FamilyActivity.vue')],
        ['/family-portal/location', '位置信息', () => import('./views/FamilyLocation.vue')],
        ['/family-portal/profile', '个人中心', () => import('./views/FamilyProfile.vue')],
        ['/family-portal/notice', '公告内容', () => import('./views/FamilyNotice.vue')]
      ] satisfies Page[]).map(page)
    },
    {
      path: '/operations',
      component: () => import('./views/OperationsShell.vue'),
      redirect: '/operations/overview',
      children: ([
        ['/operations/overview', '运营总览', () => import('./views/OperationsOverview.vue')],
        ['/operations/vitals', '心率与血压', () => import('./views/VitalsOverview.vue')],
        ['/operations/activity', '睡眠与步数', () => import('./views/ActivityOverview.vue')],
        ['/operations/locations', '位置与告警', () => import('./views/LocationOverview.vue')],
        ['/operations/life', '生活信息', () => import('./views/LifeOverview.vue')],
        ['/operations/monitoring-profiles', '监测阈值档案', () => import('./views/MonitoringProfiles.vue')],
        ['/operations/residents', '老人档案', () => import('./views/Residents.vue')],
        ['/operations/guardians', '监护人信息', () => import('./views/Guardians.vue')],
        ['/operations/notices/edit', '编辑公告', NoticeEditor, { editing: true }],
        ['/operations/notices/new', '发布公告', NoticeEditor, { editing: false }],
        ['/operations/notices/detail', '公告内容', () => import('./views/NoticeDetail.vue')],
        ['/operations/notices', '公告管理', () => import('./views/NoticeManagement.vue')],
        ['/operations/families', '家属账号', () => import('./views/FamilyAccounts.vue')],
        ['/operations/comments', '评论管理', () => import('./views/CommentManagement.vue')],
        ['/operations/invitations', '家属注册邀请码', () => import('./views/FamilyInvitations.vue')],
        ['/operations/devices', '设备管理', () => import('./views/DeviceManagement.vue')]
      ] satisfies Page[]).map(page)
    },
    { path: '/:pathMatch(.*)*', name: 'not-found', meta: { title: '页面不存在' }, component: () => import('./views/NotFound.vue') }
  ]
})

function portalHome(account: SessionAccount): string {
  return account.role === 'FAMILY' ? '/family-portal/overview' : '/operations/overview'
}

function wrongPortal(account: SessionAccount, familyPage: boolean): boolean {
  return (account.role === 'FAMILY') !== familyPage
}

// vue-router starts downloading a route's lazy chunks only after every beforeEach guard has
// settled, so the session request and the page chunk used to run back to back. Starting the
// dynamic import here lets them overlap; the module loader dedupes the router's own later call.
function prefetchRouteChunks (to: RouteLocationNormalized): void {
  for (const record of to.matched) {
    const loader: unknown = record.components?.default
    // Same test vue-router uses to tell a lazy loader from an already-resolved component.
    if (typeof loader !== 'function' || 'displayName' in loader || 'props' in loader || '__vccOpts' in loader) continue
    try {
      void Promise.resolve((loader as () => unknown)()).catch(() => undefined)
    } catch {
      // The router reports a failed chunk itself (see router.onError) when it loads the route.
    }
  }
}

router.beforeEach(async to => {
  const publicPage = to.path === '/sign-in' || to.path === '/join-family'
  const familyPage = to.matched.some(route => route.path === '/family-portal')
  prefetchRouteChunks(to)

  try {
    const account: SessionAccount | null = await loadSession()
    if (authState.setupRequired) return to.path === '/setup' ? undefined : '/setup'
    if (to.path === '/setup') return '/sign-in'
    if (publicPage) return account ? portalHome(account) : undefined
    if (!account) return '/sign-in'
    if (to.name === 'not-found') return
    if (wrongPortal(account, familyPage)) return portalHome(account)
    if (isRestrictedAdmin(account) && !['/operations/overview', '/operations'].includes(to.path)) return '/operations/overview'
  } catch (error) {
    const response = (error as { response?: { status?: number; data?: { code?: string } } } | null)?.response
    const unavailable = response?.data?.code === 'DATABASE_UNAVAILABLE'
    const transient = unavailable || response?.status === 502 || response?.status === 503 || response?.status === 504 || !response
    const account = authState.account
    if (transient && account) {
      if (!publicPage && to.name !== 'not-found' && to.path !== '/setup' && wrongPortal(account, familyPage)) return portalHome(account)
      return to.path === '/setup' ? '/sign-in' : undefined
    }
    return publicPage || (to.path === '/setup' && unavailable) ? undefined : '/sign-in'
  }
})

router.afterEach(to => { document.title = documentTitle(to.meta.title) })
router.onError(error => { recoverFromChunkError(error) })

export default router
