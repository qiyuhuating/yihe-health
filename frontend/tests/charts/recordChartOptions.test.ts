import type { EChartsType } from 'echarts/core'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyRecordChartOptions } from '@/charts/recordChartOptions'

describe('health and activity chart configuration', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    document.documentElement.classList.remove('large-text', 'reduce-motion', 'high-contrast')
  })

  it('preserves a readable line chart and reduced-motion preference', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
    const setOption = vi.fn()
    applyRecordChartOptions({ setOption } as unknown as EChartsType, {
      title: '心率记录', subtitle: '长者', labels: ['08:00'], values: [76], unit: '次/分'
    })
    expect(setOption).toHaveBeenCalledWith(expect.objectContaining({
      animation: false,
      aria: { enabled: true },
      series: [expect.objectContaining({ type: 'line', data: [76] })]
    }), true)
  })

  it('uses bars for sleep duration while keeping numeric observations unchanged', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })))
    const setOption = vi.fn()
    applyRecordChartOptions({ setOption } as unknown as EChartsType, {
      title: '睡眠时长', subtitle: '长者', labels: ['周一'], values: [6.5], unit: '小时', type: 'bar'
    })
    expect(setOption).toHaveBeenCalledWith(expect.objectContaining({
      animation: true,
      series: [expect.objectContaining({ type: 'bar', data: [6.5] })]
    }), true)
  })
  it('keeps a narrow health spike instead of discarding samples at fixed intervals', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })))
    const labels = Array.from({ length: 1000 }, (_, i) => String(i))
    const values = labels.map((_, index) => index === 137 ? 190 : 74)
    const setOption = vi.fn()
    applyRecordChartOptions({ setOption } as unknown as EChartsType, {
      title: '心率记录', subtitle: '长者', labels, values, unit: '次/分'
    })
    const option = setOption.mock.calls[0][0] as {
      xAxis: { data: string[] }, series: Array<{ data: number[] }>
    }
    expect(option.xAxis.data).toHaveLength(1000)
    expect(option.series[0].data).toHaveLength(1000)
    expect(option.series[0].data[137]).toBe(190)
  })

  it('respects large text, high contrast and reduced-motion preferences', () => {
    document.documentElement.classList.add('large-text', 'high-contrast', 'reduce-motion')
    const setOption = vi.fn()
    applyRecordChartOptions({ setOption } as unknown as EChartsType, {
      title: '睡眠时长', subtitle: '长者', labels: ['周一'], values: [6.5], unit: '小时', type: 'bar'
    })
    const option = setOption.mock.calls[0][0] as {
      animation: boolean, animationDuration: number,
      textStyle: { fontSize: number, color: string },
      title: { textStyle: { fontSize: number } }
    }
    expect(option.animation).toBe(false)
    expect(option.animationDuration).toBe(0)
    expect(option.textStyle.fontSize).toBe(18)
    expect(option.textStyle.color).toBe('#111111')
    expect(option.title.textStyle.fontSize).toBe(22)
  })
})
