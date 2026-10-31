export function formError(error: unknown): boolean {
  return !!error && typeof error === 'object' && 'errorFields' in error;
}
export function errorText(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' && error.message.trim()) {
    return error.message;
  }
  return fallback;
}
