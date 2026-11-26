import { nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { EChartsType } from 'echarts/core'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { applyRecordChartOptions, type ChartOptions } from '../charts/recordChartOptions'

interface ChartData {
  createTimes?: unknown[]
  createTime?: unknown[]
  hrs?: unknown[]
  sleep?: unknown[]
  bps?: unknown[]
  time?: string
}

const array = (value: unknown): unknown[] => Array.isArray(value) ? value : []

export function useRecordCharts() {
  const detailsVisible = ref(false)
  const chartLoading = ref(false)
  const chartError = ref('')
  const chartName = ref('')
  const primaryChartElement = shallowRef<HTMLElement | null>(null)
  const secondaryChartElement = shallowRef<HTMLElement | null>(null)
  const charts: EChartsType[] = []
  let frame: number | null = null
  let observer: ResizeObserver | null = null
  let request: AbortController | null = null
  let unmounted = false

  function disposeCharts() {
    if (frame !== null) cancelAnimationFrame(frame)
    frame = null
    observer?.disconnect()
    observer = null
    for (const chart of charts) if (!chart.isDisposed()) chart.dispose()
    charts.length = 0
  }

  function cancelRecordRequest() {
    request?.abort()
    request = null
    chartLoading.value = false
  }

  function resizeCharts() {
    if (frame !== null || unmounted || !detailsVisible.value) return
    frame = requestAnimationFrame(() => {
      frame = null
      for (const chart of charts) {
        if (chart.isDisposed()) continue
        const element = chart.getDom()
        const width = element.clientWidth
        const height = element.clientHeight
        // ResizeObserver can fire after chart.resize() updates its own canvas.
        // Avoid redrawing when the host dimensions have not changed.
        if (width > 0 && height > 0 && (chart.getWidth() !== width || chart.getHeight() !== height)) {
          chart.resize()
        }
      }
    })
  }

  async function showRecordDetails(kind: 'vitals' | 'activity', oldId: string | number, name: string) {
    cancelRecordRequest()
    const controller = new AbortController()
    request = controller
    chartName.value = name || ''
    chartError.value = ''
    detailsVisible.value = true
    chartLoading.value = true
    const active = () => request === controller && detailsVisible.value && !unmounted
    try {
      const [{ data }, { default: echarts }] = await Promise.all([
        api.post<ChartData>(`api/v1/${kind}/chart`, { oldId }, { signal: controller.signal }),
        import('../charts/chartEngine')
      ])
      if (!active()) return
      const vital = kind === 'vitals'
      const labels = array(vital ? data.createTimes : data.createTime)
      const values = array(vital ? data.hrs : data.sleep)
      const pressures = array(data.bps)
      if (!values.length && (!vital || !pressures.length)) {
        chartError.value = vital ? '暂无可展示的健康详情。' : '暂无可展示的睡眠详情。'
        return
      }
      await nextTick()
      if (!active()) return
      const subtitle = vital ? chartName.value + ' · ' + (data.time || '') : chartName.value

      function render(element: HTMLElement | null, options: ChartOptions) {
        if (!element || !active()) return
        // Capping backing-store scale at 2 avoids 3x/4x canvas memory and
        // raster work on high-density devices without changing layout size.
        const chart: EChartsType = echarts.getInstanceByDom(element) ||
          echarts.init(element, undefined, { renderer: 'canvas', devicePixelRatio: Math.min(window.devicePixelRatio || 1, 2) })
        if (!charts.includes(chart)) {
          charts.push(chart)
          if (typeof ResizeObserver !== 'undefined') {
            if (!observer) observer = new ResizeObserver(resizeCharts)
            observer.observe(element)
          }
        }
        applyRecordChartOptions(chart, options)
      }

      render(primaryChartElement.value, vital
        ? { title: '心率记录', subtitle, labels, values, unit: '次/分' }
        : { title: '睡眠时长', subtitle, labels, values, unit: '小时', type: 'bar' })
      if (vital) render(secondaryChartElement.value, { title: '血压记录', subtitle, labels, values: pressures, unit: 'mmHg' })
    } catch (error) {
      if (active()) chartError.value = apiErrorMessage(error, '详情暂时无法加载，请重试。')
    } finally {
      if (request === controller) {
        request = null
        chartLoading.value = false
      }
    }
  }

  watch(detailsVisible, visible => {
    if (!visible) {
      cancelRecordRequest()
      disposeCharts()
    }
  })
  onMounted(() => window.addEventListener('resize', resizeCharts))
  onBeforeUnmount(() => {
    unmounted = true
    cancelRecordRequest()
    window.removeEventListener('resize', resizeCharts)
    disposeCharts()
  })
  return {
    detailsVisible, chartLoading, chartError, chartName,
    primaryChartElement, secondaryChartElement,
    showRecordDetails, cancelRecordRequest, resizeCharts, disposeCharts
  }
}
