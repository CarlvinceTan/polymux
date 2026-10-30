export function formatDate(date: string, month: 'long' | 'short' = 'long'): string {
  return new Intl.DateTimeFormat('en-AU', {day: 'numeric', month, year: 'numeric', timeZone: 'UTC'}).format(new Date(`${date}T00:00:00Z`));
}

export function formatMonth(date: string): string {
  return new Intl.DateTimeFormat('en-AU', {month: 'long', year: 'numeric', timeZone: 'UTC'}).format(new Date(`${date}T00:00:00Z`));
}
