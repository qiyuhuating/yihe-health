import { nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

export function useWorkspaceFocus(onRouteChange?: () => void) {
  const route = useRoute()
  const workspaceMain = ref<HTMLElement | null>(null)

  function focusMain(): void {
    workspaceMain.value?.focus()
  }

  watch(() => route.path, async () => {
    onRouteChange?.()
    await nextTick()
    if (workspaceMain.value) {
      workspaceMain.value.scrollTop = 0
      workspaceMain.value.focus({ preventScroll: true })
    }
  })

  return { workspaceMain, focusMain }
}
