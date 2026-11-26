<template>
  <div class="family-dashboard">
    <section class="welcome-card" aria-labelledby="care-welcome-title">
      <div class="welcome-copy">
        <p class="eyebrow">{{ familyName }}，您好</p>
        <h1 id="care-welcome-title">家人的近况，<br>一目了然</h1>
        <p class="welcome-description">查看健康、活动与位置记录，<br class="desktop-break">留意家人的日常变化。</p>
        <el-button type="primary" @click="router.push('/family-portal/vitals')">查看健康记录 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </div>
      <div class="welcome-scene" aria-hidden="true"><img src="/static/img/care-family-960.webp" srcset="/static/img/care-family-640.webp 640w, /static/img/care-family-960.webp 960w" sizes="(max-width: 700px) 100vw, 40vw" alt="" width="960" height="720" fetchpriority="high" decoding="async"></div>
    </section>
    <LatestHealthPanel />
    <div class="family-daily-grid">
    <section class="announcement-panel" :aria-busy="isLoading" aria-labelledby="announcement-heading">
      <div class="panel-heading"><div><h2 id="announcement-heading">平台公告</h2><p>服务信息与照护提醒</p></div><el-button link :loading="isLoading" @click="loadNotices">刷新公告 <el-icon aria-hidden="true"><Refresh /></el-icon></el-button></div>
      <div v-loading="isLoading" class="announcement-content">
        <ul v-if="notices.length" class="announcement-list">
          <li v-for="(item, index) in notices" :key="item.textId || index">
            <span class="announcement-icon" aria-hidden="true"><el-icon aria-hidden="true"><Document /></el-icon></span><div class="announcement-copy"><h3>{{ item.title || '未命名公告' }}</h3><p>{{ item.time || '发布时间未提供' }}</p></div><el-button link :aria-label="'阅读公告：' + (item.title || '未命名公告')" @click="getDetail(item)">阅读公告 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
          </li>
        </ul>
        <div v-else class="table-empty" role="status"><img v-if="!isLoading && !loadError" src="/static/img/care-announcement-empty.webp" alt="" width="160" height="160" loading="lazy" decoding="async"><strong>{{ isLoading ? '正在加载公告' : loadError ? '公告暂时无法加载' : '暂时没有公告' }}</strong><span>{{ loadError || '平台发布的服务信息会显示在这里。' }}</span><el-button v-if="loadError" link @click="loadNotices">重新加载</el-button></div>
      </div>
      <div class="pagination-wrap" v-if="total > pageSize"><el-pagination @current-change="changePage" v-model:current-page="currentPage" :page-size="pageSize" :pager-count="5" layout="total, prev, pager, next" :total="total"></el-pagination></div>
    </section>
    <section class="family-info-row" aria-labelledby="care-record-heading">
      <h2 id="care-record-heading" class="records-heading">常用记录</h2>
      <router-link to="/family-portal/vitals" class="info-tile health-tile"><span class="tile-icon" aria-hidden="true"><el-icon aria-hidden="true"><Document /></el-icon></span><div class="tile-copy"><h3>心率与血压</h3><p>查看家人的健康记录与设备上报状态。</p></div><span class="tile-action" aria-hidden="true"><el-icon aria-hidden="true"><ArrowRight /></el-icon></span></router-link>
      <router-link to="/family-portal/activity" class="info-tile activity-tile"><span class="tile-icon" aria-hidden="true"><el-icon aria-hidden="true"><Clock /></el-icon></span><div class="tile-copy"><h3>睡眠与步数</h3><p>了解日常活动与最近上传的睡眠记录。</p></div><span class="tile-action" aria-hidden="true"><el-icon aria-hidden="true"><ArrowRight /></el-icon></span></router-link>
      <router-link to="/family-portal/location" class="info-tile location-tile"><span class="tile-icon" aria-hidden="true"><el-icon aria-hidden="true"><Location /></el-icon></span><div class="tile-copy"><h3>位置与安全</h3><p>查看最近一次位置与设备安全提醒。</p></div><span class="tile-action" aria-hidden="true"><el-icon aria-hidden="true"><ArrowRight /></el-icon></span></router-link>
    </section>
    </div>
    <p class="data-caption"><el-icon aria-hidden="true"><InfoFilled /></el-icon> 数据以手表最近一次成功上报为准；没有新记录，不代表状态正常。</p>
  </div>
</template>
<style scoped>
.family-dashboard { color: var(--care-ink); }
.welcome-card { display: grid; grid-template-columns: 1.08fr 1fr; gap: 40px; align-items: center; margin: 12px 0 36px; }
.welcome-copy { padding: 24px 0 28px; }
.eyebrow { margin: 0 0 18px; color: var(--care-text-secondary); font-size: 18px; font-weight: 500; }
.welcome-card h1 { margin: 0; color: var(--care-heading); font-family: inherit; font-size: 40px; line-height: 1.35; font-weight: 500; letter-spacing: -.015em; }
.welcome-description { max-width: 32em; margin: 18px 0 26px; color: var(--care-text-muted); font-size: 18px; line-height: 1.9; }
.welcome-copy :deep(.el-button) { min-height: 52px; padding: 12px 24px; border-radius: 8px; font-size: 18px; }
.welcome-copy :deep(.el-button i) { margin-left: 16px; }
.welcome-scene { position: relative; min-width: 0; height: 348px; overflow: hidden; border-radius: var(--care-panel-radius); }
.welcome-scene img { display: block; width: 100%; height: 100%; object-fit: contain; object-position: center; }
.family-daily-grid { display: grid; grid-template-columns: minmax(0,1.3fr) minmax(0,1fr); gap: 32px; align-items: start; }
.family-info-row { min-width: 0; display: grid; gap: 12px; }
.records-heading { margin: 0; padding: 6px 0 10px; color: var(--care-heading); font-size: var(--care-section-title); font-weight: 600; }
.info-tile { display: flex; align-items: flex-start; gap: 16px; min-width: 0; padding: 18px 20px; border: 1px solid var(--care-border); border-radius: var(--care-card-radius); background: var(--care-surface); color: var(--care-ink); text-decoration: none; transition: background-color 180ms cubic-bezier(.2,.7,.3,1), border-color 180ms cubic-bezier(.2,.7,.3,1), box-shadow 180ms cubic-bezier(.2,.7,.3,1), transform 180ms cubic-bezier(.2,.7,.3,1); }
.info-tile:hover { background: var(--care-surface-hover); border-color: var(--care-neutral-border); box-shadow: var(--care-shadow-card); }
.info-tile:hover .tile-action { transform: translateX(3px); }
.info-tile:active { transform: translateY(1px); }
.tile-icon { display: grid; place-items: center; flex: 0 0 44px; width: 44px; height: 44px; border-radius: 12px; background: var(--care-success-bg); color: var(--care-primary); font-size: 24px; }
.tile-copy { min-width: 0; flex: 1; }
.tile-copy h3 { margin: 0; color: var(--care-heading); font-size: 20px; line-height: 1.5; font-weight: 500; }
.tile-copy p { margin: 6px 0 0; color: var(--care-text-muted); font-size: 16px; line-height: 1.8; text-wrap: balance; }
.tile-action { align-self: center; flex: 0 0 20px; color: var(--care-primary); font-size: 18px; transition: transform 180ms cubic-bezier(.2,.7,.3,1); }
.announcement-panel { min-width: 0; padding: 24px 28px; border: 1px solid var(--care-border); border-radius: var(--care-panel-radius); background: var(--care-surface); }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.panel-heading h2 { margin: 0; color: var(--care-heading); font-size: var(--care-section-title); font-weight: 600; }
.panel-heading p { margin: 6px 0 0; color: var(--care-text-muted); font-size: 16px; }
.announcement-content { min-height: 120px; }
.announcement-list { padding: 0; margin: 0; list-style: none; }
.announcement-list li { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; padding: 22px 0; border-top: 1px solid var(--care-border-subtle); }
.announcement-icon { display: grid; place-items: center; flex: 0 0 28px; height: 32px; color: var(--care-primary); font-size: 22px; }
.announcement-copy { flex: 1; min-width: 120px; }
.announcement-copy h3 { margin: 0; color: var(--care-heading); font-size: 18px; font-weight: 500; line-height: 1.75; overflow-wrap: anywhere; }
.announcement-copy p { margin: 5px 0 0; color: var(--care-text-muted); font-size: 15px; }
.announcement-list :deep(.el-button) { flex-shrink: 0; }
.table-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 28px 10px; color: var(--care-text-muted); text-align: center; }
.table-empty img { display: block; object-fit: contain; }
.table-empty strong { font-size: 18px; color: var(--care-heading); }
.table-empty span { font-size: 16px; }
.pagination-wrap { display: flex; justify-content: flex-end; padding-top: 18px; overflow-x: auto; }
.data-caption { display: flex; align-items: flex-start; gap: 8px; max-width: 65em; margin: 26px 2px 0; color: var(--care-text-muted); font-size: 16px; line-height: 1.8; }
.data-caption i { margin-top: 6px; flex-shrink: 0; }
@media (min-width: 1200px) and (max-height: 900px) {
  .welcome-card { gap: 32px; margin: 0 0 24px; }
  .welcome-copy { padding: 0; }
  .eyebrow { margin-bottom: 10px; }
  .welcome-card h1 { font-size: 34px; line-height: 1.35; }
  .welcome-description { margin: 10px 0 16px; line-height: 1.7; }
  .welcome-scene { height: clamp(220px, 30dvh, 270px); }
  .announcement-panel { padding: 18px 24px; }
  .announcement-list li { padding: 14px 0; }
  .records-heading { padding: 14px 0 8px; }
  .info-tile { padding-block: 12px; }
  .tile-copy p { margin-top: 4px; line-height: 1.6; }
  .data-caption { margin-top: 16px; }
}
@media (max-width: 1180px) { .welcome-card { gap: 28px; } .welcome-card h1 { font-size: 36px; } .welcome-scene { height: 320px; } .family-daily-grid { grid-template-columns: 1fr; gap: 28px; } }
@media (max-width: 700px) { .welcome-card { grid-template-columns: 1fr; gap: 8px; margin: 0 0 28px; } .welcome-copy { padding: 4px 0 20px; } .welcome-card h1 { font-size: 32px; } .welcome-scene { height: 164px; border-radius: var(--care-panel-radius); } .welcome-scene img { object-position: center; } .welcome-description { margin: 14px 0 22px; } .family-daily-grid { gap: 24px; } .announcement-panel { padding: 20px 18px; } .announcement-list li { display: grid; grid-template-columns: 28px minmax(0,1fr); align-items: start; gap: 10px; } .announcement-list :deep(.el-button) { grid-column: 2; justify-self: start; margin-left: 0; } .panel-heading { align-items: flex-start; } .announcement-copy h3 { font-size: 18px; } .desktop-break { display: none; } }
html.large-text .care-family .welcome-card h1 { font-size: 48px; }
html.large-text .care-family .welcome-description { font-size: 21px; }
html.large-text .care-family .tile-copy h3 { font-size: 23px; }
html.large-text .care-family .tile-copy p,
html.large-text .care-family .tile-action,
html.large-text .care-family .announcement-copy h3 { font-size: 20px; }
html.large-text .care-family .announcement-copy p,
html.large-text .care-family .data-caption { font-size: 18px; }
@media (max-width: 700px) { html.large-text .care-family .welcome-card h1 { font-size: 36px; } }
</style>
<script setup lang="ts">
import { ArrowRight, Clock, Document, InfoFilled, Location, Refresh } from '@element-plus/icons-vue'
import { computed, ref } from 'vue'
import { authState } from '../auth'
import { useRouter } from 'vue-router'
import { usePagedRecords } from '@/composables/usePagedRecords'
import LatestHealthPanel from '../components/LatestHealthPanel.vue'
import type { NoticeRecord } from '../domain/notices'

const router = useRouter()
const familyName = computed(() => authState.account?.displayName || '家人')
const searchText = ref('')
const {
  records: notices, total, pageSize, currentPage, isLoading, loadError,
  load: loadNotices, changePage
} = usePagedRecords<NoticeRecord, { title: string }>({
  path: 'api/v1/notices/list',
  responseKey: 'texts',
  buildQuery: () => ({ title: searchText.value }),
  failure: '请检查服务连接后重试'
})
function getDetail(row: NoticeRecord) {
  void router.push({
    path: '/family-portal/notice',
    query: { textId: row.textId, picId: row.picId }
  })
}
</script>
