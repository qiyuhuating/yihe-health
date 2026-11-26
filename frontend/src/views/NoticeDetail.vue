<template>
  <div class="admin-notice">
    <el-button plain :icon="ArrowLeft" @click="router.push('/operations/overview')">返回运营总览</el-button>
    <article v-loading="loading" class="notice-panel">
      <div v-if="error" role="alert" class="notice-feedback"><p>{{ error }}</p><el-button plain @click="loadNotice">重新加载公告</el-button></div>
      <template v-else-if="notice">
        <header><p class="notice-label">平台公告</p><h2>{{ notice.title || '未命名公告' }}</h2><p class="notice-time">发布时间 · {{ notice.time || '暂未提供' }}</p></header>
        <div class="notice-body">{{ notice.textMain || '本公告暂无正文。' }}</div>
        <section v-if="pictures.length" class="notice-pictures" aria-label="公告图片"><img v-for="(pic, index) in pictures" :key="pic.picName" :src="noticeImageUrl(pic.picName)" :alt="'公告附图 ' + (index + 1)"></section>
      </template>
    </article>
    <section class="notice-panel" v-loading="commentsLoading" aria-label="家属留言">
      <div class="comments-heading"><h2>家属留言 <span>{{ comments.length }}</span></h2><el-button plain @click="router.push('/operations/comments')">管理评论</el-button></div>
      <div v-if="commentsError" role="alert" class="notice-feedback"><p>{{ commentsError }}</p><el-button plain @click="loadComments">重新加载留言</el-button></div>
      <p v-else-if="!commentsLoading && !comments.length" class="notice-time">暂时没有留言。</p>
      <ul v-else class="comment-list"><li v-for="(comment, index) in comments" :key="comment.saidId || index"><strong>{{ comment.personName || '家属' }}</strong><p>{{ comment.said }}</p><p v-if="comment.back" class="comment-reply">平台回复：{{ comment.back }}</p><small>赞同 · {{ comment.niceCount || 0 }}</small></li></ul>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ArrowLeft } from '@element-plus/icons-vue'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { createLatestRead } from '../composables/latestRead'
import { noticeImageUrl, type NoticeRecord, type NoticePicture, type NoticeComment, type NoticeDetailResponse, type NoticeCommentsResponse } from '../domain/notices'

const route = useRoute()
const router = useRouter()
const notice = ref<NoticeRecord | null>(null)
const pictures = ref<NoticePicture[]>([])
const comments = ref<NoticeComment[]>([])
const loading = ref(false)
const commentsLoading = ref(false)
const error = ref('')
const commentsError = ref('')
const noticeReads = createLatestRead(
  (query: { textId: unknown }, signal: AbortSignal) =>
    api.post<NoticeDetailResponse>('api/v1/notices/detail', query, { signal }),
  ({ data }) => {
    if (!data.text) throw new Error('missing notice')
    notice.value = data.text
    pictures.value = Array.isArray(data.pics) ? data.pics : []
    error.value = ''
  },
  cause => {
    notice.value = null
    pictures.value = []
    error.value = apiErrorMessage(cause, '公告暂时无法加载，请重试。')
  },
  value => { loading.value = value }
)
const commentReads = createLatestRead(
  (query: { textId: unknown }, signal: AbortSignal) =>
    api.post<NoticeCommentsResponse>('api/v1/notice-comments/list', query, { signal }),
  ({ data }) => {
    comments.value = Array.isArray(data.comments) ? data.comments : []
    commentsError.value = ''
  },
  cause => {
    comments.value = []
    commentsError.value = apiErrorMessage(cause, '留言暂时无法加载，请重试。')
  },
  value => { commentsLoading.value = value }
)
function loadNotice() {
  notice.value = null
  pictures.value = []
  return noticeReads.run({ textId: route.query.textId })
}
function loadComments() {
  comments.value = []
  return commentReads.run({ textId: route.query.textId })
}
function reload() { void loadNotice(); void loadComments() }
onMounted(reload)
watch(() => route.query.textId, reload)
onBeforeUnmount(() => { noticeReads.dispose(); commentReads.dispose() })
</script>

<style scoped>
.admin-notice { max-width: 1040px; margin: 0 auto; color: var(--care-text); }
.notice-panel { min-width: 0; margin-top: 20px; padding: 32px; border: 1px solid var(--care-border); border-radius: 16px; background: #fff; }
.notice-label { margin: 0 0 12px; color: var(--care-primary); font-size: 13px; font-weight: 600; letter-spacing: .12em; }
.notice-panel h2 { margin: 0; font-size: 24px; line-height: 1.5; overflow-wrap: anywhere; }
.notice-time, .notice-feedback { color: var(--care-text-muted); font-size: 15px; line-height: 1.8; }
.notice-body { margin-top: 28px; font-size: 17px; line-height: 2; white-space: pre-wrap; overflow-wrap: anywhere; }
.notice-pictures { display: grid; gap: 16px; margin-top: 24px; }
.notice-pictures img { display: block; max-width: 100%; height: auto; border-radius: 10px; }
.comments-heading { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.comments-heading h2 { font-size: 20px; }
.comments-heading h2 span { margin-left: 8px; font-size: 15px; font-weight: 400; color: var(--care-text-muted); }
.comment-list { list-style: none; margin: 24px 0 0; padding: 0; }
.comment-list li { padding: 20px 0; border-top: 1px solid #e8ece4; font-size: 16px; line-height: 1.8; overflow-wrap: anywhere; }
.comment-list p { margin: 8px 0; white-space: pre-wrap; }
.comment-reply { padding: 12px 16px; background: #f1f4eb; border-radius: 8px; }
.comment-list small { color: var(--care-text-muted); font-size: 14px; }
@media (max-width: 600px) { .notice-panel { padding: 22px 18px; } .notice-panel h2 { font-size: 22px; } }
</style>
