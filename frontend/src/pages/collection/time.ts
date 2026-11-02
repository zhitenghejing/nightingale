import moment from 'moment';

// G2's default time mask drops the clock from tick text; its ID retains the raw epoch.
export function formatAxisTime(text: string | number, tick?: { id?: string }): string {
  const value = tick?.id ?? text;
  const numeric = Number(value);
  const parsed = Number.isFinite(numeric) ? moment(numeric) : moment(value, moment.ISO_8601, true);
  return parsed.isValid() ? parsed.format('HH:mm') : '—';
}
