<template>
  <div class="notice-page">
    <el-button class="notice-back" link :icon="Back" @click="router.push('/family-portal/overview')">返回照护总览</el-button>
    <article v-loading="isLoading" class="notice-article" :aria-busy="isLoading">
      <div v-if="loadError" class="notice-empty" role="alert"><h1>公告暂时无法加载</h1><p>{{ loadError }}</p><el-button type="primary" @click="searchDetail">重新加载</el-button></div>
      <template v-else><p class="notice-kicker">平台公告</p><h1>{{ title || (isLoading ? '正在加载公告…' : '公告内容') }}</h1><p class="notice-time">{{ time || '发布时间未提供' }}</p><div class="notice-body">{{ textMain || (isLoading ? '' : '暂时没有正文内容。') }}</div><figure v-for="(img, index) in imgList" :key="img.picName" class="notice-attachment"><img v-if="!failedImages[img.picName]" :src="noticeImageUrl(img.picName)" :alt="'公告附件 ' + (index + 1)" loading="lazy" @error="failedImages[img.picName] = true"><p v-else>附件图片暂时无法显示。</p></figure></template>
    </article>
    <section v-if="!isLoading && !loadError" class="notice-comments" aria-labelledby="notice-comment-title">
      <h2 id="notice-comment-title">留言交流</h2>
      <el-form :model="formSaid" @submit.prevent="onSubmit"><el-form-item label="您的留言（最多30字）"><el-input aria-label="您的留言（最多30字）" type="textarea" :rows="3" maxlength="30" v-model="formSaid.said" placeholder="写下您想咨询或分享的内容"></el-input></el-form-item><p v-if="submitError" class="notice-error" role="alert">{{ submitError }}</p><el-button type="primary" :loading="submitting" :disabled="!formSaid.said.trim()" @click="onSubmit">发表留言</el-button><p v-if="submitSuccess" class="notice-success" role="status">{{ submitSuccess }}</p></el-form>
      <div class="comment-list" v-loading="commentsLoading"><p v-if="commentsError" role="alert">{{ commentsError }} <el-button link @click="searchSaid">重新加载</el-button></p><p v-else-if="!tableData.length" class="comment-empty">还没有留言，欢迎分享您的想法。</p><article v-for="(item, index) in tableData" :key="item.saidId || index" class="comment-item"><div class="comment-heading"><strong>{{ item.personName || '家属' }}</strong><span>{{ item.saidTime || '' }}</span></div><p>{{ item.said }}</p><div v-if="item.back" class="comment-reply"><strong>平台回复</strong><p>{{ item.back }}</p></div><el-button link :aria-pressed="String(item.good == 1)" :loading="likingId === item.saidId" @click="toggleCommentLike(item)">{{ item.good == 1 ? '取消赞同' : '赞同' }} · {{ item.niceCount || 0 }}</el-button></article></div>
    </section>
  </div>
</template>
<script setup lang="ts">
import { Back } from '@element-plus/icons-vue'
import { onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { createLatestRead } from '../composables/latestRead'
import { noticeImageUrl, type NoticePicture, type NoticeComment, type NoticeDetailResponse, type NoticeCommentsResponse } from '../domain/notices'

const router = useRouter()
const route = useRoute()
const title = ref('')
const time = ref('')
const textMain = ref('')
const imgList = ref<NoticePicture[]>([])
const failedImages = reactive<Record<string, boolean>>({})
const tableData = ref<NoticeComment[]>([])
const formSaid = reactive({ said: '' })
const isLoading = ref(false)
const loadError = ref('')
const commentsLoading = ref(false)
const commentsError = ref('')
const submitting = ref(false)
const submitError = ref('')
const submitSuccess = ref('')
const likingId = ref<number | string | null>(null)
let noticeRevision = 0

const details = createLatestRead(
  (query: { textId: unknown }, signal: AbortSignal) =>
    api.post<NoticeDetailResponse>('api/v1/notices/detail', query, { signal }),
  ({ data }) => {
    if (!data?.text) throw new Error('missing notice')
    title.value = data.text.title || '未命名公告'
    time.value = data.text.time || ''
    textMain.value = data.text.textMain || ''
    imgList.value = Array.isArray(data.pics) ? data.pics : []
    loadError.value = ''
  },
  cause => { loadError.value = apiErrorMessage(cause, '请检查服务连接后重试。') },
  loading => { isLoading.value = loading }
)
const comments = createLatestRead(
  (query: { textId: unknown }, signal: AbortSignal) =>
    api.post<NoticeCommentsResponse>('api/v1/notice-comments/list', query, { signal }),
  ({ data }) => {
    tableData.value = Array.isArray(data.comments) ? data.comments : []
    commentsError.value = ''
  },
  cause => {
    tableData.value = []
    commentsError.value = apiErrorMessage(cause, '留言暂时无法加载。')
  },
  loading => { commentsLoading.value = loading }
)
function searchDetail() {
  if (!route.query.textId) {
    loadError.value = '缺少公告信息，请返回总览重新选择。'
    return
  }
  Object.keys(failedImages).forEach(key => { delete failedImages[key] })
  title.value = ''
  textMain.value = ''
  imgList.value = []
  return details.run({ textId: route.query.textId })
}
function searchSaid() {
  if (!route.query.textId) return
  return comments.run({ textId: route.query.textId })
}
async function onSubmit() {
  if (!formSaid.said.trim() || submitting.value || !route.query.textId) return
  const textId = route.query.textId
  const revision = noticeRevision
  submitting.value = true
  submitError.value = ''
  submitSuccess.value = ''
  try {
    const { data } = await api.post<Envelope>('api/v1/notice-comments/create', {
      said: formSaid.said.trim(), textId
    })
    accepted(data)
    if (revision !== noticeRevision) return
    formSaid.said = ''
    submitSuccess.value = '留言已发表。'
    await searchSaid()
  } catch (error) {
    if (revision !== noticeRevision) return
    submitError.value = apiErrorMessage(error, '留言未能发表，内容已保留，请重试。')
  } finally { if (revision === noticeRevision) submitting.value = false }
}
async function toggleCommentLike(item: NoticeComment) {
  const revision = noticeRevision
  if (likingId.value !== null) return
  likingId.value = item.saidId
  submitError.value = ''
  try {
    const { data } = await api.post<Envelope>('api/v1/notice-comments/toggle-like', { saidId: item.saidId })
    accepted(data)
    if (revision !== noticeRevision) return
    await searchSaid()
  } catch (error) {
    if (revision !== noticeRevision) return
    submitError.value = apiErrorMessage(error, '赞同操作未完成，请重试。')
  } finally { if (revision === noticeRevision) likingId.value = null }
}
function reload() {
  noticeRevision++
  formSaid.said = ''
  submitError.value = ''
  submitSuccess.value = ''
  submitting.value = false
  likingId.value = null
  void searchDetail(); void searchSaid()
}
onMounted(reload)
watch(() => route.query.textId, reload)
onBeforeUnmount(() => { details.dispose(); comments.dispose() })
</script>
<style scoped>
.notice-back { margin-bottom: 16px; }
.notice-article, .notice-comments { padding: 32px 40px; border: 1px solid var(--care-border); border-radius: 16px; background: #fff; }
.notice-article { min-height: 260px; }
.notice-kicker { margin: 0 0 12px; color: #496151; font-size: 17px; }
.notice-article h1 { margin: 0; color: var(--care-brand-dark); font-size: 32px; line-height: 1.6; overflow-wrap: anywhere; }
.notice-time { color: #59665f; font-size: 17px; }
.notice-body { margin-top: 28px; padding-top: 24px; border-top: 1px solid #e8ece4; color: #33453c; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 19px; line-height: 1.95; }
.notice-attachment { margin: 24px 0 0; }
.notice-attachment img { display: block; width: auto; max-width: 100%; height: auto; border-radius: 10px; }
.notice-comments { margin-top: 22px; }
.notice-comments h2 { margin: 0 0 24px; font-size: 24px; color: var(--care-heading); }
.notice-comments :deep(.el-form-item__label) { float: none; display: block; text-align: left; }
.notice-comments :deep(.el-form-item__content) { margin-left: 0 !important; }
.comment-list { margin-top: 28px; min-height: 80px; }
.comment-item { padding: 22px 0; border-top: 1px solid #e8ece4; }
.comment-item > p, .comment-reply p { margin: 10px 0; white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; }
.comment-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; }
.comment-heading span, .comment-empty { color: #59665f; font-size: 16px; }
.comment-reply { padding: 14px 18px; border-left: 3px solid #b3c6a8; background: #f4f7ef; border-radius: 0 8px 8px 0; }
.notice-error { color: var(--care-danger); } .notice-success { color: #236b3d; }
.notice-empty { text-align: center; }
@media (max-width: 600px) { .notice-article, .notice-comments { padding: 24px; } .notice-article h1 { font-size: 26px; } }
html.large-text .care-family .notice-body { font-size: 22px; }
</style>
