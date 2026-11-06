import dayjs from 'dayjs'

export const dateTimeFormat = 'YYYY-MM-DD HH:mm'

export function formatDateTime(value: string | number | null | undefined): string {
  if (value == null) return '—'
  const date = dayjs(value)
  return date.isValid() ? date.format(dateTimeFormat) : '—'
}
