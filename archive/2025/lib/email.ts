// lib/email.ts
export const isValidEmail = (e?: string | null): boolean =>
  !!e && /.+@.+\..+/.test(e);
