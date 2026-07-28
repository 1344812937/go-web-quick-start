export type LogDateRange = [Date, Date]

export interface LogDateRangeShortcut {
  /** Shortcut label displayed in the date picker panel. */
  text: string
  /** Calendar range produced when the shortcut is selected. */
  value: () => LogDateRange
}

function startOfDay(value: Date): Date {
  const result = new Date(value)
  result.setHours(0, 0, 0, 0)
  return result
}

function endOfDay(value: Date): Date {
  const result = new Date(value)
  result.setHours(23, 59, 59, 999)
  return result
}

function calendarRange(startDaysAgo: number, endDaysAgo = 0): LogDateRange {
  const now = new Date()
  const start = new Date(now)
  const end = new Date(now)
  start.setDate(start.getDate() - startDaysAgo)
  end.setDate(end.getDate() - endDaysAgo)
  return [startOfDay(start), endOfDay(end)]
}

function currentWeekRange(): LogDateRange {
  const now = new Date()
  const daysSinceMonday = (now.getDay() + 6) % 7
  return calendarRange(daysSinceMonday)
}

function currentMonthRange(): LogDateRange {
  const now = new Date()
  return [new Date(now.getFullYear(), now.getMonth(), 1), endOfDay(now)]
}

export function todayLogRange(): LogDateRange {
  return calendarRange(0)
}

export const logDateDefaultTimes: LogDateRange = [
  new Date(2000, 0, 1, 0, 0, 0),
  new Date(2000, 0, 1, 23, 59, 59),
]

export const logDateRangeShortcuts: LogDateRangeShortcut[] = [
  { text: '今天', value: todayLogRange },
  { text: '昨天', value: () => calendarRange(1, 1) },
  { text: '近三天', value: () => calendarRange(2) },
  { text: '近五天', value: () => calendarRange(4) },
  { text: '本周', value: currentWeekRange },
  { text: '近一周', value: () => calendarRange(6) },
  { text: '本月', value: currentMonthRange },
]
