import { formatAxisTime } from './time';

test('time axes accept both epoch milliseconds and formatted chart tick text', () => {
  const local = new Date(2026, 9, 25, 16, 53).getTime();
  expect(formatAxisTime(local)).toBe('16:53');
  expect(formatAxisTime(String(local))).toBe('16:53');
  expect(formatAxisTime('2026-10-25 16:53:00')).toBe('16:53');
  expect(formatAxisTime('2026-10-25', { id: String(local) })).toBe('16:53');
  expect(formatAxisTime('unavailable')).toBe('—');
});
