import { onMounted, ref } from 'vue'

const LARGE_TEXT_KEY = 'large_text'
const HIGH_CONTRAST_KEY = 'high_contrast'

function readFlag(key: string): boolean {
  try {
    return localStorage.getItem(key) === 'true'
  } catch {
    return false
  }
}

const largeText = ref(readFlag(LARGE_TEXT_KEY))
const highContrast = ref(readFlag(HIGH_CONTRAST_KEY))
let storageListenerInstalled = false

function applyDisplayPreferences(): void {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('large-text', largeText.value)
  document.documentElement.classList.toggle('high-contrast', highContrast.value)
}

function ensureStorageSync(): void {
  if (storageListenerInstalled || typeof window === 'undefined') return
  storageListenerInstalled = true
  window.addEventListener('storage', event => {
    if (event.storageArea !== localStorage) return
    if (event.key === null) {
      largeText.value = false
      highContrast.value = false
      applyDisplayPreferences()
    } else if (event.key === LARGE_TEXT_KEY) {
      largeText.value = event.newValue === 'true'
      applyDisplayPreferences()
    } else if (event.key === HIGH_CONTRAST_KEY) {
      highContrast.value = event.newValue === 'true'
      applyDisplayPreferences()
    }
  })
}

function setLargeText(value: boolean): void {
  largeText.value = value
  try {
    localStorage.setItem(LARGE_TEXT_KEY, String(value))
  } catch {
    // Keep the active session preference when storage is unavailable.
  }
  applyDisplayPreferences()
}

function setHighContrast(value: boolean): void {
  highContrast.value = value
  try {
    localStorage.setItem(HIGH_CONTRAST_KEY, String(value))
  } catch {
    // Keep the active session preference when storage is unavailable.
  }
  applyDisplayPreferences()
}

export function useTextSizePreference() {
  onMounted(() => {
    ensureStorageSync()
    applyDisplayPreferences()
  })
  return {
    largeText,
    highContrast,
    setLargeText,
    setHighContrast,
    toggleTextSize: () => setLargeText(!largeText.value),
    toggleHighContrast: () => setHighContrast(!highContrast.value)
  }
}
