import { errorText, formError } from './errors';

const fallback = 'Unable to load collection data';

it.each([
  [400, 'address is outside collector policy'],
  [403, 'collection request not permitted'],
  [409, 'target ident is already registered'],
  [503, 'collection operation failed'],
])('shows the API rejection for HTTP %s from a silent Umi request', (status, message) => {
  // The shared request interceptor rejects plain objects for silence: true.
  const rejection = { status, message, name: message, silence: true, data: { err: message } };
  expect(errorText(rejection, fallback)).toBe(message);
});

it('preserves native errors and falls back for unknown rejections', () => {
  expect(errorText(new Error('Network unavailable'), fallback)).toBe('Network unavailable');
  for (const rejection of [undefined, null, {}, { message: null }, { message: 400 }, { message: '' }, { message: '  ' }]) {
    expect(errorText(rejection, fallback)).toBe(fallback);
  }
});

it('keeps field validation errors in the form', () => {
  expect(formError({ errorFields: [{ name: ['address'], errors: ['Required'] }] })).toBe(true);
  expect(formError({ status: 400, message: 'address is outside collector policy' })).toBe(false);
});
