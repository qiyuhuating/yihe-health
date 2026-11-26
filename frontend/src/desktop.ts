import { api } from './api/client'
import { invoke, isTauri } from '@tauri-apps/api/core'
import { getCurrentWindow } from '@tauri-apps/api/window'
import { ElMessage, ElMessageBox } from 'element-plus'

interface SetupResult {
  status: number
  data?: { message?: string }
}

export async function createAdministrator(account: {
  name: string
  phone: string
  password: string
}) {
  if (!isTauri()) return api.post('/api/v1/setup', account)
  const result = await invoke<SetupResult>('setup_admin', { account })
  if (result.status >= 400) {
    const error = new Error(result.data?.message || '暂时无法创建账号') as Error & {
      response?: SetupResult
    }
    error.response = result
    throw error
  }
  return result
}

function hasOpenModalOrOverlay(): boolean {
  return Array.from(document.querySelectorAll<HTMLElement>('.el-overlay, [role="dialog"]')).some(element => {
    const style = window.getComputedStyle(element)
    return element.getClientRects().length > 0 &&
      element.getAttribute('aria-hidden') !== 'true' &&
      style.display !== 'none' && style.visibility !== 'hidden'
  })
}

export function installDesktopKeys(): () => void {
  if (!isTauri()) return () => {}

  const onExternalClick = (event: MouseEvent) => {
    if (event.defaultPrevented || (event.button !== 0 && event.button !== 1)) return
    const element = event.target
    if (!(element instanceof Element)) return
    const link = element.closest<HTMLAnchorElement>('a[href]')
    if (!link) return
    let destination: URL
    try {
      destination = new URL(link.href, window.location.href)
    } catch {
      event.preventDefault()
      ElMessage.warning('无效的网页链接')
      return
    }
    if (destination.origin === window.location.origin) return
    event.preventDefault()
    event.stopPropagation()
    if (destination.protocol !== 'https:' || destination.username || destination.password ||
        destination.port || !destination.hostname.includes('.') ||
        /^(?:localhost|127\\.|10\\.|192\\.168\\.)/.test(destination.hostname)) {
      ElMessage.warning('已阻止非 HTTPS 或非公开域名的外部跳转')
      return
    }
    const blocked = ['doubleclick.net', 'googlesyndication.com', 'googleadservices.com',
      'adservice.google.com', 'taboola.com', 'outbrain.com']
    if (blocked.some(domain => destination.hostname === domain || destination.hostname.endsWith('.' + domain))) {
      ElMessage.warning('已拦截广告跳转域名')
      return
    }
    void (async () => {
      try {
        await ElMessageBox.confirm(
          `即将离开颐和并使用系统浏览器打开：\n${destination.hostname}\n${destination.href}\n仅在信任目标网站时继续。`,
          '确认打开外部链接',
          { confirmButtonText: '打开网站', cancelButtonText: '留在颐和', type: 'warning' }
        )
      } catch { return }
      try {
        await invoke('open_external_link', { url: destination.href })
      } catch (error) {
        ElMessage.error(error instanceof Error ? error.message : String(error))
      }
    })()
  }

  window.addEventListener('click', onExternalClick, true)
  window.addEventListener('auxclick', onExternalClick, true)

  const onKeyDown = async (event: KeyboardEvent) => {
    if (event.repeat || !['F11', 'Escape'].includes(event.key)) return
    if (event.key === 'Escape' && hasOpenModalOrOverlay()) return

    const desktop = getCurrentWindow()
    try {
      const fullscreen = await desktop.isFullscreen()
      if (event.key === 'F11' || fullscreen) {
        event.preventDefault()
        await desktop.setFullscreen(event.key === 'F11' ? !fullscreen : false)
      }
    } catch { /* Fullscreen errors must not interrupt active dialogs. */ }
  }

  const onKeyDownListener = (event: KeyboardEvent) => { void onKeyDown(event) }
  window.addEventListener('keydown', onKeyDownListener)
  return () => {
    window.removeEventListener('keydown', onKeyDownListener)
    window.removeEventListener('click', onExternalClick, true)
    window.removeEventListener('auxclick', onExternalClick, true)
  }
}
