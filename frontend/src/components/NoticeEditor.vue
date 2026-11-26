<template>
  <div class="notice-editor" :aria-busy="loading || saving">
    <div v-if="loadError" class="collection-feedback" role="alert"><span>{{ loadError }}</span><el-button plain @click="loadNotice">重新加载公告</el-button></div>
    <section class="editor-main collection-panel" v-loading="loading">
      <div class="editor-section-heading"><div><h2>公告内容</h2><p>清晰的标题与简短的正文，让家属更容易了解重点。</p></div></div>
      <el-form ref="editorForm" :model="form" :rules="rules" label-position="top">
        <el-form-item label="公告标题" for="notice-title" prop="title"><el-input id="notice-title" v-model="form.title" maxlength="20" placeholder="用一句话说明本次提醒" :disabled="saving || loading || !!loadError"></el-input><p class="editor-counter">{{ form.title.length }} / 20 字</p></el-form-item>
        <el-form-item label="公告正文" for="notice-body" prop="textMain"><el-input id="notice-body" type="textarea" :rows="4" v-model="form.textMain" maxlength="50" placeholder="写下需要家属知悉的简要提醒" :disabled="saving || loading || !!loadError"></el-input><p class="editor-counter">{{ form.textMain.length }} / 50 字</p></el-form-item>
      </el-form>
      <section class="editor-attachments" aria-labelledby="attachment-heading"><div class="editor-section-heading"><div><h2 id="attachment-heading">补充图片</h2><p>图片可选，请上传与本次公告直接相关的内容。</p></div></div><el-upload action="/api/v1/image/saveImage" :headers="uploadHeaders" :with-credentials="true" :file-list="attachments" :disabled="saving || loading || !!loadError" :limit="10" accept="image/jpeg,image/png" list-type="picture" :before-upload="beforeUpload" :on-change="filesChanged" :on-success="uploadSuccess" :on-error="uploadError" :on-remove="filesRemoved" :on-exceed="uploadExceeded"><el-button plain :icon="Plus" :disabled="saving || loading || !!loadError">添加公告图片</el-button><template #tip><p  class="collection-caption">最多10张 JPG / PNG 图片，每张不超过500KB。{{ uploadsPending ? '图片仍在上传或未成功，请处理后再发布。' : '' }}</p></template></el-upload></section>
      <p v-if="saveError" class="editor-error" role="alert">{{ saveError }}</p>
      <footer class="editor-actions"><span>确认预览内容后再{{ editing ? '保存' : '发布' }}。</span><el-button type="primary" :loading="saving" :disabled="loading || !!loadError || uploadsPending" @click="submit">{{ editing ? '保存公告' : '发布公告' }}</el-button></footer>
    </section>
    <aside class="editor-side"><section class="notice-preview collection-panel" aria-label="公告实时预览"><p class="collection-eyebrow">家属阅读预览</p><h2>{{ form.title || '这里显示公告标题' }}</h2><p class="preview-time">{{ editing ? '保存后更新公告内容' : '发布后显示发布时间' }}</p><div class="preview-body">{{ form.textMain || '正文会在这里显示，方便您确认语气与重点。' }}</div><img v-for="file in successfulAttachments" :key="file.uid || file.name" :src="file.url" alt="公告附图预览"></section><section class="editor-guidance"><img src="/static/img/care-courtyard.webp" alt="" width="960" height="411"><div><h3>简洁一点，关怀更清楚</h3><p>先说明事项，再交代家属需要留意的内容。避免模糊时间和未经确认的健康结论。</p></div></section></aside>
  </div>
</template>
<script setup lang="ts">
import { Plus } from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type UploadFile, type UploadFiles } from 'element-plus'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { authState } from '../auth'
import { noticeEditTarget, noticeImageUrl, type NoticeDetailResponse } from '../domain/notices'

const props = withDefaults(defineProps<{ editing?: boolean }>(), { editing: false })
const route = useRoute()
const router = useRouter()
const uploadHeaders = computed(() => ({ [authState.csrfHeader]: authState.csrfToken }))
const form = ref({ title: '', textMain: '' })
// The save target must come from the same load that produced the content.
// Reading the route at submit time lets a late response from record A be
// written into record B after the route moved on.
const loadedTextId = ref<string | null>(null)
let loadRevision = 0
let disposed = false
function currentTextId(): string {
  const value = route.query.textId
  return typeof value === 'string' ? value : Array.isArray(value) ? String(value[0] ?? '') : ''
}
const attachments = ref<UploadFile[]>([])
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const saveError = ref('')
const baseline = ref(JSON.stringify({ title: '', textMain: '', names: [] as string[] }))
const editorForm = ref<FormInstance | null>(null)
const rules = {
  title: [{ required: true, whitespace: true, message: '请输入公告标题', trigger: 'blur' }],
  textMain: [{ required: true, whitespace: true, message: '请输入公告正文', trigger: 'blur' }]
}
const signature = computed(() => JSON.stringify({
  title: form.value.title, textMain: form.value.textMain,
  names: attachments.value.map(file => file.name)
}))
const dirty = computed(() => signature.value !== baseline.value)
const successfulAttachments = computed(() => attachments.value.filter(file => file.status === 'success'))
const uploadsPending = computed(() => attachments.value.some(file => file.status !== 'success'))
async function confirmLeave() {
  if (!dirty.value) return true
  if (saving.value || attachments.value.some(file => ['ready','uploading'].includes(file.status))) {
    ElMessage.warning('正在保存或处理图片，请稍候再离开')
    return false
  }
  try {
    await ElMessageBox.confirm('尚有未保存的内容，离开后将丢失。', '离开编辑', {
      confirmButtonText: '离开编辑', cancelButtonText: '继续编辑', type: 'warning'
    })
    return true
  } catch { return false }
}
onBeforeRouteLeave(confirmLeave)
onBeforeRouteUpdate(confirmLeave)

async function loadNotice() {
  const revision = ++loadRevision
  const textId = currentTextId()
  loading.value = true
  loadError.value = ''
  loadedTextId.value = null
  try {
    const { data } = await api.post<NoticeDetailResponse>('api/v1/notices/detail', { textId })
    if (disposed || revision !== loadRevision) return
    if (!data.text || data.text.picId == null) throw new Error('missing notice')
    form.value = { title: data.text.title || '', textMain: data.text.textMain || '' }
    attachments.value = (data.pics || []).map((pic, index) => ({
      uid: -(index + 1), name: pic.picName,
      url: noticeImageUrl(pic.picName), status: 'success' as const
    }))
    loadedTextId.value = textId
    baseline.value = signature.value
  } catch (error) {
    if (disposed || revision !== loadRevision) return
    loadError.value = apiErrorMessage(error, '公告暂时无法加载，请重试。')
  } finally {
    if (!disposed && revision === loadRevision) loading.value = false
  }
}
function beforeUpload(file: File) {
  if (!['image/jpeg','image/png'].includes(file.type)) {
    ElMessage.error('请选择 JPG 或 PNG 图片')
    return false
  }
  if (file.size > 500 * 1024) {
    ElMessage.error('每张图片不能超过500KB')
    return false
  }
  if (attachments.value.filter(item => item.name === file.name).length > 1) {
    ElMessage.error('已有同名图片，请更换文件名后上传')
    return false
  }
  return true
}
function filesChanged(_file: UploadFile, files: UploadFiles) { attachments.value = files }
function filesRemoved(_file: UploadFile, files: UploadFiles) { attachments.value = files }
function uploadSuccess(response: { returnCode?: string; filename?: string } | null, file: UploadFile, files: UploadFiles) {
  if (!response || response.returnCode !== '1111' || !response.filename) {
    file.status = 'fail'
    ElMessage.error(apiErrorMessage({ cause: response }, '图片未能上传，请删除后重试'))
  } else file.name = response.filename
  attachments.value = files
}
function uploadError(error: Error, _file: UploadFile, files: UploadFiles) {
  attachments.value = files
  ElMessage.error(apiErrorMessage(error, '图片暂时无法上传，请删除后重试'))
}
function uploadExceeded() { ElMessage.warning('最多添加10张公告图片') }
async function submit() {
  if (saving.value || loading.value || loadError.value || uploadsPending.value) return
  const target = noticeEditTarget(loadedTextId.value, currentTextId())
  if (props.editing && target === null) return
  const valid = await editorForm.value?.validate().catch(() => false)
  if (!valid || saving.value) return
  saving.value = true
  saveError.value = ''
  const params: Record<string, unknown> = {
    title: form.value.title.trim(),
    textMain: form.value.textMain.trim(),
    fileListName: successfulAttachments.value.map(file => file.name)
  }
  if (props.editing) params.textId = target
  try {
    const { data } = await api.post<Envelope>(
      props.editing ? 'api/v1/notices/update' : 'api/v1/notices/create', params
    )
    accepted(data)
    baseline.value = signature.value
    ElMessage.success(props.editing ? '公告已保存' : '公告已发布')
    await router.push('/operations/notices')
  } catch (error) {
    saveError.value = apiErrorMessage(error, '公告暂时未能保存，内容已保留，请稍后重试。')
  } finally { saving.value = false }
}
onMounted(() => { if (props.editing) void loadNotice() })
onBeforeUnmount(() => { disposed = true; loadRevision++ })
watch(() => route.query.textId, () => { if (props.editing) void loadNotice() })
</script>
<style scoped>
.notice-editor { display: grid; grid-template-columns: minmax(0, 1.45fr) minmax(260px, 1fr); gap: 24px; align-items: start; }
.editor-main { padding: 28px; }
.editor-section-heading { display: flex; align-items: flex-start; gap: 14px; margin-bottom: 24px; }
.editor-section-heading h2 { margin: 0; color: var(--care-heading); font-size: 20px; line-height: 1.6; }
.editor-section-heading p, .editor-counter { color: var(--care-text-muted); font-size: 14px; line-height: 1.8; margin: 4px 0 0; }
.editor-counter { text-align: right; }
.editor-attachments { margin-top: 28px; padding-top: 24px; border-top: 1px solid var(--care-border-soft); }
.editor-actions { display: flex; flex-wrap: wrap; gap: 16px; align-items: center; justify-content: space-between; margin-top: 28px; padding-top: 24px; border-top: 1px solid var(--care-border-soft); }
.editor-actions span { color: var(--care-text-muted); font-size: 14px; }
.editor-error { color: var(--care-danger); line-height: 1.8; }
.editor-side { min-width: 0; }
.notice-preview { padding: 28px; }
.notice-preview h2 { margin: 16px 0 10px; color: var(--care-heading); font-size: 24px; line-height: 1.65; overflow-wrap: anywhere; }
.preview-time { color: var(--care-text-muted); font-size: 13px; }
.preview-body { min-height: 100px; margin-top: 24px; color: #33453c; font-size: 17px; line-height: 2; white-space: pre-wrap; overflow-wrap: anywhere; }
.notice-preview img { display: block; width: 100%; height: auto; margin-top: 16px; border-radius: 8px; }
.editor-guidance { margin-top: 20px; overflow: hidden; border: 1px solid var(--care-border); border-radius: 16px; background: #edf1e5; }
.editor-guidance > img { display: block; width: 100%; height: 130px; object-fit: cover; }
.editor-guidance > div { padding: 20px 24px; }
.editor-guidance h3 { margin: 0 0 8px; font-size: 17px; color: var(--care-heading); }
.editor-guidance p { margin: 0; color: var(--care-text-muted); font-size: 14px; line-height: 1.9; }
@media (max-width: 1100px) { .notice-editor { grid-template-columns: 1fr; } .editor-side { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; } .editor-guidance { margin-top: 0; } }
@media (max-width: 600px) { .editor-main, .notice-preview { padding: 22px 18px; } .editor-side { grid-template-columns: 1fr; } .editor-actions .el-button { width: 100%; } }
</style>
