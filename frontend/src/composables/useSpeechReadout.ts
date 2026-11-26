import { onBeforeUnmount, ref } from 'vue'

// Speech is opt-in. The native browser/Tauri speech engine speaks the text only
// after a user click; no third-party speech API or new dependency is used.
export function useSpeechReadout() {
  const isSupported = typeof window !== 'undefined' &&
    'speechSynthesis' in window && typeof SpeechSynthesisUtterance !== 'undefined'
  const isSpeaking = ref(false)
  let current: SpeechSynthesisUtterance | null = null

  function stop(): void {
    const wasSpeaking = current !== null
    current = null
    isSpeaking.value = false
    if (wasSpeaking && isSupported) window.speechSynthesis.cancel()
  }

  function play(text: string): void {
    const content = text.trim()
    if (!isSupported || !content) return
    stop()
    const utterance = new SpeechSynthesisUtterance(content)
    current = utterance
    utterance.lang = 'zh-CN'
    utterance.rate = 0.92
    const finish = () => {
      if (current === utterance) {
        current = null
        isSpeaking.value = false
      }
    }
    utterance.onend = finish
    utterance.onerror = finish
    isSpeaking.value = true
    try {
      window.speechSynthesis.speak(utterance)
    } catch {
      finish()
    }
  }

  function toggle(text: string): void {
    if (isSpeaking.value) stop()
    else play(text)
  }

  onBeforeUnmount(stop)
  return { isSupported, isSpeaking, play, stop, toggle }
}
