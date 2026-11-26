export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch {
      // Embedded or insecure contexts may need the synchronous fallback.
    }
  }
  const previousFocus = document.activeElement
  const input = document.createElement('textarea')
  input.value = text
  input.readOnly = true
  input.style.position = 'fixed'
  input.style.opacity = '0'
  document.body.appendChild(input)
  try {
    input.select()
    if (!document.execCommand('copy')) throw new Error('Clipboard copy rejected')
  } finally {
    input.remove()
    if (previousFocus instanceof HTMLElement) previousFocus.focus()
  }
}
