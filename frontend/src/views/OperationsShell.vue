<template>
  <div class="care-app">
    <a class="skip-link" href="#admin-main" @click.prevent="focusMain()">跳到主要内容</a>
    <NavTop></NavTop>
    <div class="workspace-layout">
      <Slider class="workspace-sidebar"></Slider>
      <main ref="workspaceMain" id="admin-main" class="workspace-main" tabindex="-1">
        <div class="workspace-tabs" v-if="openTab.length">
          <el-tabs :model-value="activeIndex" @tab-click="tabClick" @tab-remove="tabRemove">
            <el-tab-pane v-for="item in openTab" :key="item.route" :label="item.name" :name="item.route" :closable="item.route !== '/operations/overview'"></el-tab-pane>
          </el-tabs>
        </div>
        <section class="workspace-content">
          <database-notice />
          <header v-if="showPageIntro" class="view-intro">
            <p class="view-breadcrumb">工作台 <el-icon><ArrowRight /></el-icon> {{ route.meta.title }}</p>
            <h1>{{ route.meta.title }}</h1>
            <p class="view-description">{{ pageDescription }}</p>
          </header>
          <router-view v-slot="{ Component }"><transition name="care-view" mode="out-in"><ErrorBoundary :key="route.path"><component :is="Component" /></ErrorBoundary></transition></router-view>
        </section>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ArrowRight } from '@element-plus/icons-vue'
import type { TabsPaneContext } from 'element-plus'
import ErrorBoundary from '@/components/ErrorBoundary.vue'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Slider from '@/components/Slider.vue'
import NavTop from '@/components/Header.vue'
import store from '@/store'

const route = useRoute()
const router = useRouter()
const workspaceMain = ref<HTMLElement | null>(null)
const openTab = computed(() => store.state.openTab)
const activeIndex = computed(() => store.state.activeIndex)
const noIntro = ['/operations/overview', '/operations/devices', '/operations/invitations']
const showPageIntro = computed(() => !noIntro.includes(route.path))
const descriptions: Record<string, string> = {
  '/operations/vitals': '查看近期心率与单项血压记录，异常状态以设备上报为准。',
  '/operations/activity': '查看手表上报的睡眠与每日步数记录。',
  '/operations/locations': '查看设备最近一次上报的位置与安全告警。',
  '/operations/life': '查看手表上报的生活信息。',
  '/operations/monitoring-profiles': '维护监测阈值档案；设备注册与在线状态请前往设备管理。',
  '/operations/residents': '管理老人基础档案与关联设备。',
  '/operations/guardians': '查看监护人资料与联系电话。',
  '/operations/families': '查看家属账号与关联信息。',
  '/operations/notices/edit': '编辑公告内容并预览发布信息。',
  '/operations/notices/new': '创建面向家属的服务公告。',
  '/operations/notices/detail': '查阅公告详情与图片内容。',
  '/operations/notices': '查看和维护已发布公告。',
  '/operations/comments': '查看公告评论并处理互动内容。'
}
const pageDescription = computed(() => descriptions[route.path] || '在这里查看和管理照护信息。')
function focusMain() { workspaceMain.value?.focus() }
function tabClick(tab: TabsPaneContext) {
  if (typeof tab.paneName === 'string' && tab.paneName !== route.path) {
    void router.push({ path: tab.paneName })
  }
}
function tabRemove(target: string | number) {
  const routeName = String(target)
  if (routeName === '/operations/overview') return
  store.removeTab(routeName)
  if (activeIndex.value === routeName) {
    const next = openTab.value[openTab.value.length - 1]
    void router.push({ path: next?.route || '/operations/overview' })
  }
}
function addCurrentTab(nextRoute: ReturnType<typeof useRoute>) {
  if (!nextRoute.path || nextRoute.path === '/operations') return
  store.addTab({ route: nextRoute.path, name: (typeof nextRoute.meta.title === 'string' && nextRoute.meta.title ? nextRoute.meta.title : '运营总览') })
  store.activate(nextRoute.path)
}
onMounted(() => addCurrentTab(route))
watch(() => route.path, async () => {
  addCurrentTab(route)
  await nextTick()
  if (workspaceMain.value) {
    workspaceMain.value.scrollTop = 0
    workspaceMain.value.focus({ preventScroll: true })
  }
})
</script>

<style scoped>
.skip-link { position: fixed; top: -80px; left: 16px; z-index: 3000; padding: 14px 20px; border-radius: 8px; background: #fff; color: var(--care-brand-dark); }
.skip-link:focus { top: 12px; }
.care-app { display: grid; grid-template-rows: auto minmax(0, 1fr); height: 100vh; height: 100dvh; overflow: hidden; background: var(--care-canvas); }
.workspace-layout { display: grid; grid-template-columns: 232px minmax(0, 1fr); min-height: 0; overflow: hidden; }
.workspace-sidebar { min-width: 0; min-height: 0; overflow-y: auto; }
.workspace-main { min-width: 0; min-height: 0; overflow: auto; overscroll-behavior: contain; scroll-padding-block: 20px; padding: clamp(18px, 2vh, 26px) clamp(20px, 2.2vw, 48px) 28px; }
.workspace-tabs { width: 100%; margin: 0 auto 28px; border-bottom: 1px solid #dce4db; padding: 0; }
.workspace-tabs :deep(.el-tabs__header) { margin: 0; border: 0; }
.workspace-tabs :deep(.el-tabs__nav-wrap::after) { display: none; }
.workspace-tabs :deep(.el-tabs__item) { height: 48px; line-height: 48px; color: var(--care-muted); }
.workspace-tabs :deep(.el-tabs__item.is-active) { color: var(--care-primary); font-weight: 500; }
.workspace-tabs :deep(.el-tabs__active-bar) { background: var(--care-primary); }
.workspace-content :deep(.admin-record-dialog) { display: flex; flex-direction: column; max-width: 640px; max-height: calc(100vh - 48px); margin: 24px auto !important; }
.workspace-content :deep(.admin-record-dialog .el-dialog__header) , .workspace-content :deep(.admin-record-dialog .el-dialog__footer) { flex-shrink: 0; }
.workspace-content :deep(.admin-record-dialog .el-dialog__header) { padding-right: 76px; }
.workspace-content :deep(.admin-record-dialog .el-dialog__body) { min-height: 0; overflow-y: auto; }
.workspace-content :deep(.admin-record-dialog .el-dialog__headerbtn) { top: 8px; right: 12px; display: grid; place-items: center; width: 48px; height: 48px; font-size: 20px; }
.workspace-content { min-width: 0; width: 100%; margin-right: auto; margin-left: auto; }
.view-intro { margin: 0 0 20px; }
.view-breadcrumb { margin: 0 0 10px; color: var(--care-text-muted); font-size: 13px; }
.view-breadcrumb i { margin: 0 5px; font-size: 9px; }
.view-intro h1 { margin: 0; color: var(--care-text); font-size: 28px; line-height: 1.25; font-weight: 500; letter-spacing: -.02em; }
.view-description { margin: 8px 0 0; color: #64756c; font-size: 14px; line-height: 1.65; }
@media (max-width: 900px) {
  .workspace-layout { grid-template-columns: minmax(0, 1fr); grid-template-rows: auto minmax(0, 1fr); }
  .workspace-sidebar { max-height: 40dvh; }
  .workspace-main { padding: 20px 20px 30px; }
}
@media (max-width: 600px) {
  .workspace-main { padding: 16px 14px 26px; }
  .view-intro { margin-bottom: 14px; }
  .view-intro h1 { font-size: 24px; }
  .view-description { font-size: 14px; }
  .workspace-tabs { margin-bottom: 20px; padding: 0; }
}
</style>
