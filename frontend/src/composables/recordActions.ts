import { ElMessage, ElMessageBox } from 'element-plus'
import { accepted, api, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'

// 同一请求（路径+内容）未完成时忽略重复触发，避免双击确定键造成重复创建。
const inFlight = new Set<string>()

// Writes and prompts use Vue 3 services directly instead of receiving a component instance.
export async function writeRecord(
  path: string,
  body: unknown,
  success: string,
  refresh: () => void | Promise<unknown>,
  failure = '请求未完成，请重试'
): Promise<boolean> {
  const key = path + JSON.stringify(body)
  if (inFlight.has(key)) return false
  inFlight.add(key)
  try {
    try {
      const { data } = await api.post<Envelope>(path, body)
      accepted(data)
    } catch (error) {
      ElMessage({ type: 'error', message: apiErrorMessage(error, failure) })
      return false
    }
    ElMessage({ type: 'success', message: success })
    try {
      await refresh()
    } catch {
      ElMessage({ type: 'warning', message: '操作已成功，请手动刷新列表。' })
    }
    return true
  } finally {
    inFlight.delete(key)
  }
}

export async function deleteRecord(
  path: string,
  body: unknown,
  prompt: string,
  refresh: () => void | Promise<unknown>
): Promise<boolean> {
  try {
    await ElMessageBox.confirm(prompt, '提示', {
      confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning'
    })
  } catch {
    return false
  }
  return writeRecord(path, body, '删除信息成功', refresh, '删除失败，请稍后重试')
}
