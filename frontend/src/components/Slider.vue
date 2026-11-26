<template>
  <nav class="care-sidebar" aria-label="管理端导航">
    <button class="navigation-toggle" type="button" :aria-expanded="expanded ? 'true' : 'false'" aria-controls="admin-navigation" @click="expanded = !expanded"><span><el-icon aria-hidden="true"><Menu /></el-icon> 功能导航</span><span>{{ expanded ? '收起' : '展开' }} <el-icon aria-hidden="true"><ArrowUp v-if="expanded" /><ArrowDown v-else /></el-icon></span></button>
    <div class="sidebar-label">工作空间</div>
    <el-menu ref="navigationMenu" :default-active="route.path" :default-openeds="openedGroups" unique-opened id="admin-navigation" :class="['care-menu', { 'is-expanded': expanded }]" router>
      <el-menu-item index="/operations/overview"><el-icon aria-hidden="true"><Menu /></el-icon><template #title><span >运营总览</span></template></el-menu-item>
      <el-menu-item v-if="!restrictedDuty" index="/operations/devices"><el-icon aria-hidden="true"><Clock /></el-icon><template #title><span >设备管理</span></template></el-menu-item>
      <el-sub-menu v-if="!restrictedDuty" index="health">
        <template #title ><el-icon aria-hidden="true"><Document /></el-icon><span>健康与安全</span></template>
        <el-menu-item index="/operations/vitals">心率与血压</el-menu-item>
        <el-menu-item index="/operations/activity">睡眠与步数</el-menu-item>
        <el-menu-item index="/operations/locations">位置与告警</el-menu-item>
        <el-menu-item index="/operations/life">生活信息</el-menu-item>
      </el-sub-menu>
      <el-sub-menu v-if="!restrictedDuty" index="records">
        <template #title ><el-icon aria-hidden="true"><User /></el-icon><span>档案管理</span></template>
        <el-menu-item index="/operations/residents">老人档案</el-menu-item>
        <el-menu-item index="/operations/guardians">监护人信息</el-menu-item>
        <el-menu-item index="/operations/monitoring-profiles">监测阈值档案</el-menu-item>
        <el-menu-item index="/operations/families">家属账号</el-menu-item>
        <el-menu-item index="/operations/invitations">家属注册邀请码</el-menu-item>
      </el-sub-menu>
      <el-sub-menu v-if="!restrictedDuty" index="service">
        <template #title ><el-icon aria-hidden="true"><Message /></el-icon><span>公告与互动</span></template>
        <el-menu-item index="/operations/notices/new">发布公告</el-menu-item>
        <el-menu-item index="/operations/notices">公告管理</el-menu-item>
        <el-menu-item index="/operations/comments">评论管理</el-menu-item>
      </el-sub-menu>
    </el-menu>
    <div class="sidebar-note"><span class="note-dot"></span><span>照护信息请以设备上报为准</span></div>
  </nav>
</template>

<script setup lang="ts">
import { ArrowDown, ArrowUp, Clock, Document, Menu, Message, User } from '@element-plus/icons-vue'
import { computed, nextTick, ref, watch } from 'vue'
import type { MenuInstance } from 'element-plus'
import { useRoute } from 'vue-router'
import { authState } from '@/auth'
import { isRestrictedAdmin } from '@/domain/permissions'

const route = useRoute()
const navigationMenu = ref<MenuInstance | null>(null)
const expanded = ref(false)
const restrictedDuty = computed(() => isRestrictedAdmin(authState.account))
const groups: Record<string, string[]> = {
  health: ['/operations/vitals', '/operations/activity', '/operations/locations', '/operations/life'],
  records: ['/operations/residents', '/operations/guardians', '/operations/monitoring-profiles', '/operations/families', '/operations/invitations'],
  service: ['/operations/notices/new', '/operations/notices', '/operations/comments', '/operations/notices/edit', '/operations/notices/detail']
}
const openedGroups = computed(() => Object.keys(groups).filter(group => groups[group].includes(route.path)))
watch(() => route.path, async () => {
  expanded.value = false
  await nextTick()
  const group = Object.keys(groups).find(key => groups[key].includes(route.path))
  if (group) navigationMenu.value?.open(group)
})
</script>

<style scoped>
.care-sidebar { display: flex; flex-direction: column; min-height: 100%; background: var(--care-brand-dark); }
.navigation-toggle { display: none; }
.sidebar-label { padding: 24px 22px 12px; color: #c3d5c7; font-size: 13px; font-weight: 600; letter-spacing: .1em; }
.care-menu { border-right: 0; padding: 0 10px; background: transparent; }
.care-menu :deep(.el-menu) { background: transparent; }
.care-menu .el-menu-item, .care-menu :deep(.el-sub-menu__title) { height: 48px; line-height: 48px; margin: 3px 0; border-radius: 8px; color: #e0ebe0; font-size: 15px; }
.care-menu .el-menu-item .el-icon, .care-menu :deep(.el-sub-menu__title .el-icon) { color: #c3d5c7; font-size: 17px; }
.care-menu .el-menu-item:hover, .care-menu :deep(.el-sub-menu__title:hover) { background: #2b5143; color: #fff; }
.care-menu .el-menu-item.is-active { background: #e9efdf; color: var(--care-brand-dark); font-weight: 600; box-shadow: inset 3px 0 0 var(--care-primary); }
.care-menu .el-menu-item.is-active i { color: var(--care-brand-dark); }
.care-menu .el-menu-item:focus-visible, .care-menu :deep(.el-sub-menu__title:focus-visible) { outline: 3px solid #e9efdf; outline-offset: -3px; }
.care-menu :deep(.el-menu-item-group__title) { display: none; }
.care-menu :deep(.el-sub-menu .el-menu-item) { min-width: 0; padding-left: 48px !important; }
.sidebar-note { display: flex; gap: 8px; align-items: flex-start; margin: 28px 16px 20px; padding: 12px; border: 1px solid #587365; border-radius: 9px; color: #d7e3d8; font-size: 13px; line-height: 1.65; }
.note-dot { width: 7px; height: 7px; margin-top: 6px; flex: 0 0 7px; border-radius: 50%; background: #c1d899; }
@media (max-width: 900px) {
  .care-sidebar { border-bottom: 1px solid #587365; }
  .sidebar-label, .sidebar-note { display: none; }
  .navigation-toggle { display: flex; justify-content: space-between; align-items: center; width: 100%; min-height: 56px; padding: 0 20px; background: var(--care-brand-dark); border: 0; color: #e0ebe0; font: inherit; font-size: 15px; cursor: pointer; }
  .navigation-toggle:focus-visible { outline: 3px solid #e9efdf; outline-offset: -3px; }
  .navigation-toggle .el-icon { margin: 0 5px; }
  .care-menu { display: none; padding-bottom: 12px; }
  .care-menu.is-expanded { display: block; }
}
</style>
