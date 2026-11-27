<template>
  <div class="announcement-admin">
    <section class="announcement-list-panel">
      <header class="announcement-toolbar">
        <div><h2>公告动态</h2><p>查看、检索并维护已发布的公告。</p></div>
        <div class="announcement-actions">
          <form class="announcement-query" role="search" @submit.prevent="loadNotices()"><label class="table-search-label" for="manage-notice-search">按标题查找</label><el-input id="manage-notice-search" v-model="searchText" placeholder="搜索公告标题" clearable @clear="loadNotices">
            <template #append><el-button  :icon="Search" aria-label="搜索公告" native-type="submit"></el-button></template>
          </el-input></form>
          <el-button type="primary" :icon="Plus" @click="openNoticeEditor">添加公告</el-button>
        </div>
      </header>
      <div v-if="loadError" class="collection-feedback" role="alert">{{ loadError }}<el-button plain @click="loadNotices">重新加载公告</el-button></div>
      <el-table class="managed-notices-desktop" v-loading="isLoading" :data="notices" @row-dblclick="getDetail" row-class-name="clickable-row">
        <template #empty><div  class="care-empty-state"><strong>暂无已发布公告</strong><span>发布后的公告会显示在这里。</span></div></template>
        <el-table-column type="index" label="序号" width="80" align="center"></el-table-column>
        <el-table-column prop="title" label="公告标题" min-width="260" show-overflow-tooltip></el-table-column>
        <el-table-column prop="time" label="发布时间" width="190" align="center"></el-table-column>
        <el-table-column label="操作" width="180" align="center">
          <template #default="scope" >
            <el-button size="small" @click="handleEdit(scope.$index, scope.row)">编辑</el-button>
            <el-button size="small" type="danger" :disabled="deletingId !== null" @click="handleDelete(scope.$index, scope.row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="managed-notices-mobile" v-loading="isLoading"><article v-for="item in notices" :key="item.textId" class="managed-notice"><h3>{{ item.title || '未命名公告' }}</h3><p>{{ item.time || '暂无发布时间' }}</p><div><el-button plain @click="getDetail(item)">阅读</el-button><el-button type="primary" plain @click="handleEdit(0, item)">编辑</el-button><el-button type="danger" plain :disabled="deletingId !== null" @click="handleDelete(0, item)">删除</el-button></div></article><div v-if="!notices.length" class="collection-empty"><strong>{{ isLoading ? '正在加载公告…' : loadError ? '公告暂时无法加载' : '暂无匹配的公告' }}</strong><p>您可以调整关键词，或发布一条新的公告。</p></div></div>
      <div v-if="total > pageSize" class="pagination-wrap">
        <el-pagination @size-change="handleSizeChange" @current-change="changePage" v-model:current-page="currentPage" :page-size="pageSize" layout="total, prev, pager, next" :pager-count="5" :total="total"></el-pagination>
      </div>
    </section>
    <el-dialog title="展示图片预览" v-model="previewVisible"><img class="gallery-preview" :src="previewUrl" alt="选中的展示图片" decoding="async"></el-dialog>

    <details class="upload-panel"><summary class="image-maintenance-summary">更多展示图片设置<span>公告附图请在公告编辑中添加</span></summary><div class="image-maintenance-body">
      <div class="upload-heading"><div><h2>公告图片</h2><p>维护独立的展示图片，JPG 格式，单张不超过8MB。</p></div></div>
      <el-upload ref="galleryUpload" class="upload-demo" :disabled="gallerySaving" :on-change="galleryChanged" :on-error="galleryError" action="/api/v1/image/saveImage" :with-credentials="true" :headers="uploadHeaders" :on-preview="handlePreview" :on-remove="handleRemove" :on-success="handleAvatarSuccess" :before-upload="beforeAvatarUpload" multiple :limit="10" :file-list="galleryFiles" list-type="picture">
        <el-button size="small" type="primary" :icon="UploadFilled">选择图片</el-button>
        <template #tip><div  class="el-upload__tip">最多上传 10 张图片。</div></template>
      </el-upload>
      <div class="upload-footer"><el-button type="primary" :loading="gallerySaving" :disabled="!galleryFiles.length || galleryFiles.some(file => file.status !== 'success')" @click="saveGallery">提交图片</el-button></div>
    </div></details>
  </div>
</template>
<style scoped>
.announcement-admin { display: grid; grid-template-columns: minmax(0, 1fr); gap: 20px; min-width: 0; color: var(--care-text); }
.announcement-list-panel, .upload-panel { min-width: 0; max-width: 100%; box-sizing: border-box; padding: 22px; border: 1px solid var(--care-border); border-radius: var(--care-panel-radius); background: var(--care-surface); box-shadow: var(--care-shadow-sm); }
.announcement-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 20px; min-width: 0; margin-bottom: 18px; }
.announcement-toolbar h2, .upload-heading h2 { margin: 0; font-size: 18px; font-weight: 600; }
.announcement-toolbar p, .upload-heading p { margin: 6px 0 0; color: var(--care-text-muted); font-size: 14px; line-height: 1.65; }
.announcement-query { flex: 1; min-width: 0; }
.announcement-actions { display: flex; align-items: flex-end; gap: 16px; width: min(100%, 540px); min-width: 0; }
.announcement-actions .el-input { min-width: 0; }
.announcement-actions > .el-button { min-height: var(--care-control-height); }
.announcement-list-panel .el-table { width: 100% !important; }
.pagination-wrap { display: flex; justify-content: flex-end; padding-top: 18px; }
.upload-panel { padding-bottom: 18px; }
.upload-footer { display: flex; justify-content: flex-end; margin-top: 18px; }
@media (max-width: 760px) { .announcement-list-panel, .upload-panel { padding: 18px 14px; } .announcement-toolbar { align-items: stretch; flex-direction: column; } .announcement-actions { width: 100%; } .pagination-wrap { justify-content: flex-start; overflow-x: auto; } }
@media (max-width: 480px) { .announcement-actions { align-items: stretch; flex-direction: column; } .announcement-actions .el-input { width: 100%; } }
</style>
<script setup lang="ts">
import { Plus, Search, UploadFilled } from '@element-plus/icons-vue'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type UploadFile, type UploadFiles, type UploadInstance } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { authState } from '../auth'
import type { NoticeRecord } from '../domain/notices'

const router = useRouter()
const uploadHeaders = computed(() => ({ [authState.csrfHeader]: authState.csrfToken }))
const searchText = ref('')
const {
  records: notices, isLoading, loadError, total,
  pageSize, currentPage, load: loadNotices,
  handleSizeChange, changePage
} = usePagedRecords<NoticeRecord, { title: string }>({
  path: 'api/v1/notices/list',
  responseKey: 'texts',
  buildQuery: () => ({ title: searchText.value }),
  failure: '公告暂时无法更新，请重试。'
})
const deletingId = ref<number | string | null>(null)
const gallerySaving = ref(false)
const previewVisible = ref(false)
const previewUrl = ref('')
const galleryFiles = ref<UploadFile[]>([])
const galleryUpload = ref<UploadInstance | null>(null)
function handleEdit(_index: number, row: NoticeRecord) {
  void router.push({ path: '/operations/notices/edit', query: { textId: row.textId } })
}
async function saveGallery() {
  if (gallerySaving.value || !galleryFiles.value.length ||
      galleryFiles.value.some(file => file.status !== 'success')) return
  gallerySaving.value = true
  try {
    const { data } = await api.post<Envelope>('api/v1/gallery/replace', {
      fileListName: galleryFiles.value.map(file => file.name)
    })
    accepted(data)
    ElMessage.success('展示图片已保存')
    galleryUpload.value?.clearFiles()
    galleryFiles.value = []
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '展示图片未能保存，选择的图片已保留，请重试'))
  } finally { gallerySaving.value = false }
}
function galleryChanged(_file: UploadFile, files: UploadFiles) { galleryFiles.value = files }
function handleRemove(_file: UploadFile, files: UploadFiles) { galleryFiles.value = files }
function handlePreview(file: UploadFile) {
  previewUrl.value = file.url ?? ''
  previewVisible.value = true
}
function handleAvatarSuccess(res: { returnCode?: string; filename?: string } | null, file: UploadFile, files: UploadFiles) {
  if (!res || res.returnCode !== '1111' || !res.filename) {
    file.status = 'fail'
    ElMessage.error(apiErrorMessage({ cause: res }, '图片未能上传，请删除后重试'))
  } else file.name = res.filename
  galleryFiles.value = files
}
function galleryError(error: Error, _file: UploadFile, files: UploadFiles) {
  galleryFiles.value = files
  ElMessage.error(apiErrorMessage(error, '图片暂时无法上传，请重试'))
}
function beforeAvatarUpload(file: File): boolean {
  if (file.type !== 'image/jpeg') {
    ElMessage.error('请选择 JPG 图片')
    return false
  }
  if (file.size > 8 * 1024 * 1024) {
    ElMessage.error('图片大小不能超过8MB')
    return false
  }
  return true
}
function getDetail(row: NoticeRecord) {
  void router.push({ path: '/operations/notices/detail', query: { textId: row.textId } })
}
async function handleDelete(_index: number, row: NoticeRecord) {
  if (deletingId.value !== null) return
  deletingId.value = row.textId
  try {
    try {
      await ElMessageBox.confirm('删除后将无法继续阅读这条公告，是否删除？', '删除公告', {
        confirmButtonText: '删除公告', cancelButtonText: '保留公告', type: 'warning'
      })
    } catch { return }
    const { data } = await api.post<Envelope>('api/v1/notices/delete', { textId: row.textId })
    accepted(data)
    ElMessage.success('公告已删除')
    await loadNotices(true)
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '公告暂时无法删除，请重试'))
  } finally { deletingId.value = null }
}
function openNoticeEditor() { void router.push('/operations/notices/new') }
</script>
