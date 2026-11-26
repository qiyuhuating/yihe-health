// 把表格数据导出为 Excel 可直接打开的 CSV（UTF-8 BOM），并防御 CSV 公式注入。
export interface CsvColumn { key: string; label: string }

function cell(value: unknown): string {
  // Preserve pre-refactor value conversion (including dates and custom values).
  // eslint-disable-next-line @typescript-eslint/no-base-to-string
  let text = value === null || value === undefined ? '' : String(value)
  // 以 = + - @ 制表符/回车开头的单元格会被 Excel 当作公式执行。
  if (/^[\s\uFEFF]*[=+\-@]/u.test(text) || /^[\t\r\n]/.test(text)) text = `'${text}`
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
}

export function toCsv(columns: CsvColumn[], rows: Record<string, unknown>[]): string {
  const lines = [columns.map(c => cell(c.label)).join(',')]
  for (const row of rows) lines.push(columns.map(c => cell(row[c.key])).join(','))
  return '﻿' + lines.join('\r\n')
}

export function downloadCsv(filename: string, columns: CsvColumn[], rows: Record<string, unknown>[]): void {
  const url = URL.createObjectURL(new Blob([toCsv(columns, rows)], { type: 'text/csv;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  try {
    link.click()
  } catch (error) {
    URL.revokeObjectURL(url)
    throw error
  } finally {
    link.remove()
  }
  // Browser downloads may acquire the blob asynchronously after click returns.
  setTimeout(() => URL.revokeObjectURL(url), 30_000)
}
