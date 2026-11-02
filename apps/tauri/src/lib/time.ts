/** Time text as the app writes it, ported from the reference time formatter. */
export function progressiveTime(date: Date, separator = false, now = new Date()): string {
  const hour = date.getHours();
  const time = `${hour % 12 === 0 ? 12 : hour % 12}.${String(date.getMinutes()).padStart(2, '0')} ${
    hour < 12 ? 'am' : 'pm'
  }`;
  const sameDay = date.toDateString() === now.toDateString();
  if (sameDay) return time;
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) {
    return separator ? `Yesterday ${time}` : 'Yesterday';
  }
  const monday = new Date(now);
  monday.setDate(now.getDate() - ((now.getDay() + 6) % 7));
  monday.setHours(0, 0, 0, 0);
  if (date >= monday && date <= now) {
    const weekday = date.toLocaleDateString('en-GB', { weekday: 'long' });
    return separator ? `${weekday} ${time}` : weekday;
  }
  const day = String(date.getDate()).padStart(2, '0');
  const month = String(date.getMonth() + 1).padStart(2, '0');
  if (date.getFullYear() === now.getFullYear()) {
    const text = `${day}/${month}`;
    return separator ? `${text}, ${time}` : text;
  }
  const long = date.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
  return separator ? `${long} ${time}` : long;
}
