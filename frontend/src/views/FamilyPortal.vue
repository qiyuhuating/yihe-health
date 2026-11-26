<template>
  <div class="care-app care-family">
    <a class="skip-link" href="#care-main" @click.prevent="focusMain()">跳转到主要内容</a>
    <NavTop></NavTop>
    <div class="workspace-layout">
      <FamilySlider class="workspace-sidebar"></FamilySlider>
      <main ref="workspaceMain" id="care-main" class="workspace-main" tabindex="-1">
        <section class="workspace-content">
          <database-notice />
          <header v-if="showPageIntro" class="view-intro">
            <p class="view-breadcrumb">家属空间 <el-icon><ArrowRight /></el-icon> {{ route.meta.title }}</p>
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
import ErrorBoundary from '@/components/ErrorBoundary.vue'
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import FamilySlider from '@/components/FamilySlider.vue'
import NavTop from '@/components/Header.vue'

const route = useRoute()
const workspaceMain = ref<HTMLElement | null>(null)
const pagesWithoutIntro = ['/family-portal/overview', '/family-portal/notice']
const descriptions: Record<string, string> = {
  '/family-portal/vitals': '查看家人最近上报的心率与单项血压记录。',
  '/family-portal/activity': '查看设备上报的睡眠与每日步数记录。',
  '/family-portal/location': '查看设备最近一次上报的位置与安全告警。',
  '/family-portal/profile': '查看和维护当前家属账号资料。'
}
const showPageIntro = computed(() => !pagesWithoutIntro.includes(route.path))
const pageDescription = computed(() => descriptions[route.path] || '查看家人的照护信息与服务内容。')
function focusMain() { workspaceMain.value?.focus() }
watch(() => route.path, async () => {
  await nextTick()
  if (workspaceMain.value) {
    workspaceMain.value.scrollTop = 0
    workspaceMain.value.focus({ preventScroll: true })
  }
})
</script>

<style scoped>
.care-app { display: grid; grid-template-rows: auto minmax(0, 1fr); height: 100vh; height: 100dvh; overflow: hidden; background: var(--care-canvas); }
.workspace-layout { display: grid; grid-template-columns: 232px minmax(0, 1fr); min-height: 0; overflow: hidden; }
.workspace-sidebar { min-width: 0; min-height: 0; overflow-y: auto; }
.workspace-main { min-width: 0; min-height: 0; overflow: auto; overscroll-behavior: contain; scroll-padding-block: 20px; padding: clamp(18px, 2vh, 26px) clamp(20px, 2.2vw, 48px) 28px; }
.workspace-content { min-width: 0; width: 100%; margin-right: auto; margin-left: auto; }
.skip-link { position: fixed; top: -100px; left: 18px; padding: 12px 18px; z-index: 3000; border-radius: 6px; background: var(--care-brand-dark); color: #fff; }
.skip-link:focus { top: 12px; }
.workspace-main:focus-visible { outline: 2px solid #215c4a; outline-offset: -4px; }
.view-intro { margin: 0 0 20px; }
.view-breadcrumb { margin: 0 0 10px; color: #59665f; font-size: 15px; }
.view-breadcrumb i { margin: 0 5px; font-size: 9px; }
.view-intro h1 { margin: 0; color: var(--care-text); font-size: 28px; line-height: 1.25; font-weight: 500; letter-spacing: -.02em; }
.view-description { margin: 8px 0 0; color: var(--care-text-muted); font-size: 17px; line-height: 1.65; }
@media (max-width: 900px) {
  .workspace-layout { display: grid; grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; min-height: 0; overflow: hidden; }
  .workspace-sidebar { grid-column: 1; grid-row: 2; }
  .workspace-main { grid-column: 1; grid-row: 1; min-height: 0; overflow: auto; overscroll-behavior: contain; -webkit-overflow-scrolling: touch; scroll-padding-block: 20px; padding: 24px 24px 28px; }
}
@media (max-width: 600px) {
  .workspace-main { padding: 20px 16px 24px; }
  .view-intro { margin-bottom: 14px; }
  .view-intro h1 { font-size: 24px; }
  .view-description { font-size: 16px; }
}

html.large-text .care-family .view-breadcrumb { font-size: 17px; }
html.large-text .care-family .view-description { font-size: 19px; }
html.large-text .care-family .workspace-main { padding-right: 24px; padding-left: 24px; }
</style>
