export const residentFields = [
  'name', 'bothdate', 'sex', 'phone', 'room',
  'illness', 'warn', 'foodLike', 'foodHate', 'drag', 'customerId'
] as const

export function editableResident(draft: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(residentFields.map(field => [field, draft[field]]))
}
