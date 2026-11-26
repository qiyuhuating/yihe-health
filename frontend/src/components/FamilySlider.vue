<template>
  <nav class="care-sidebar" aria-label="家人照护导航">
    <div class="sidebar-label">家人照护</div>
    <el-menu :default-active="route.path" class="care-menu" router>
      <el-menu-item index="/family-portal/overview" aria-label="照护总览"><el-icon aria-hidden="true"><Menu /></el-icon><span class="nav-label-full">照护总览</span><span class="nav-label-short">总览</span></el-menu-item>
      <el-menu-item index="/family-portal/vitals" aria-label="心率与血压"><el-icon aria-hidden="true"><Document /></el-icon><span class="nav-label-full">心率与血压</span><span class="nav-label-short">健康</span></el-menu-item>
      <el-menu-item index="/family-portal/activity" aria-label="睡眠与步数"><el-icon aria-hidden="true"><Clock /></el-icon><span class="nav-label-full">睡眠与步数</span><span class="nav-label-short">活动</span></el-menu-item>
      <el-menu-item index="/family-portal/location" aria-label="位置与安全"><el-icon aria-hidden="true"><Location /></el-icon><span class="nav-label-full">位置与安全</span><span class="nav-label-short">位置</span></el-menu-item>
      <el-menu-item index="/family-portal/profile" aria-label="个人中心"><span class="nav-profile-icon" aria-hidden="true"><svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="7" r="3.5"/><path d="M5 21v-2a7 7 0 0 1 14 0v2Z"/></svg></span><span class="nav-label-full">个人中心</span><span class="nav-label-short">我的</span></el-menu-item>
    </el-menu>
    <div class="sidebar-moment" aria-hidden="true"><img src="/static/img/care-botanical.webp" alt="" width="440" height="660" loading="lazy" decoding="async"><p>让关爱<br>成为日常</p></div>
    <div class="sidebar-note"><el-icon aria-hidden="true"><Clock /></el-icon><span>数据以最近一次<br>设备上报为准</span></div>
  </nav>
</template>
<script setup lang="ts">
import { Clock, Document, Location, Menu } from '@element-plus/icons-vue'
import { useRoute } from 'vue-router'
const route = useRoute()
</script>
<style scoped>
.care-sidebar { position: relative; display: flex; flex-direction: column; min-height: 100%; background: var(--care-brand-dark); color: var(--care-sidebar-text); }
.sidebar-label { padding: 28px 26px 16px; color: var(--care-sidebar-muted); font-size: 16px; font-weight: 500; letter-spacing: .08em; }
.care-menu {
  --el-menu-bg-color: transparent;
  --el-menu-text-color: var(--care-sidebar-text);
  --el-menu-hover-text-color: #ffffff;
  --el-menu-hover-bg-color: var(--care-sidebar-hover);
  --el-menu-active-color: var(--care-brand-dark);
  padding: 0 14px;
  border-right: 0;
  background: transparent;
}
.care-menu .el-menu-item { position: relative; display: flex; align-items: center; height: 58px; margin: 6px 0; padding: 0 18px !important; border-radius: 9px; color: var(--care-sidebar-text); transition: background-color var(--care-motion-fast) var(--care-ease), color var(--care-motion-fast) var(--care-ease); }
.care-menu .el-menu-item .el-icon { margin-right: 14px; color: var(--care-sidebar-muted); font-size: 22px; }
.care-menu .el-menu-item:hover,
.care-menu .el-menu-item:focus { background: var(--care-sidebar-hover); color: #ffffff; }
.care-menu .el-menu-item.is-active { background: var(--care-sidebar-active-bg); color: var(--care-brand-dark); font-weight: 700; box-shadow: inset 3px 0 0 var(--care-primary); }
.care-menu .el-menu-item.is-active .el-icon,
.care-menu .el-menu-item.is-active i { color: var(--care-primary); }
.care-menu .el-menu-item:focus-visible { outline: var(--care-focus-width) solid var(--care-sidebar-accent); outline-offset: 2px; }
.nav-profile-icon { display: grid; place-items: center; width: 24px; height: 24px; margin-right: 14px; }
.nav-label-short { display: none; }
.sidebar-moment { position: relative; min-height: 210px; flex: 1; overflow: hidden; }
.sidebar-moment img { position: absolute; width: 180px; height: 270px; bottom: -66px; left: -18px; object-fit: contain; opacity: .3; }
.sidebar-moment p { position: absolute; left: 28px; bottom: 16px; margin: 0; color: var(--care-sidebar-text); font-size: 20px; line-height: 1.9; letter-spacing: .12em; }
.sidebar-note { display: flex; gap: 10px; align-items: flex-start; margin: 12px 24px 24px; padding-top: 18px; border-top: 1px solid var(--care-sidebar-border); color: var(--care-sidebar-muted); font-size: 14px; line-height: 1.7; }
.sidebar-note .el-icon { margin-top: 5px; font-size: 16px; }
@media (max-width: 900px) {
  .care-sidebar { position: static; min-height: 0; border-top: 1px solid var(--care-border); background: var(--care-surface); padding-bottom: env(safe-area-inset-bottom); }
  .sidebar-label, .sidebar-note, .sidebar-moment, .nav-label-full { display: none; }
  .nav-label-short { display: inline; }
  .care-menu { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); padding: 6px; background: var(--care-surface); }
  .care-menu::before, .care-menu::after { display: none; }
  .nav-profile-icon { margin-right: 0; }
  .care-menu .el-menu-item { display: flex; flex-direction: column; justify-content: center; gap: 4px; height: auto; min-height: 64px; margin: 0 2px; padding: 6px 0 !important; color: var(--care-text-secondary); line-height: 1.35; text-align: center; font-size: 16px; }
  .care-menu .el-menu-item .el-icon { margin: 0; color: var(--care-text-secondary); font-size: 23px; }
  .care-menu .el-menu-item:hover, .care-menu .el-menu-item:focus { background: var(--care-surface-hover); color: var(--care-brand-dark); }
  .care-menu .el-menu-item.is-active { background: var(--care-success-bg); color: var(--care-brand-dark); box-shadow: none; }
  .care-menu .el-menu-item.is-active::after { content: ''; position: absolute; top: 0; left: 30%; right: 30%; height: 3px; border-radius: 2px; background: var(--care-primary); }
  .care-menu .el-menu-item:focus-visible { outline-color: var(--care-focus-color); }
}
</style>
