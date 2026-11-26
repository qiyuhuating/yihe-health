import { onBeforeUnmount, onMounted, ref } from 'vue'

/** A reactive matchMedia breakpoint shared with responsive layouts. */
export function useCollectionLayout(breakpoint = 1100) {
  const query = window.matchMedia(`(max-width: ${breakpoint}px)`)
  const mobileLayout = ref(query.matches)
  const update = (event: MediaQueryListEvent) => { mobileLayout.value = event.matches }

  onMounted(() => {
    query.addEventListener('change', update)
    mobileLayout.value = query.matches
  })
  onBeforeUnmount(() => query.removeEventListener('change', update))
  return mobileLayout
}
