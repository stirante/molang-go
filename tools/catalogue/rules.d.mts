// Types for rules.mjs, for the TypeScript that imports it (the extension's tests).
export const DOCS_ORIGIN: string;
export const DOCS_BASE: string;
export function docsUrl(name: string): string;
export const VERIFICATION_STATES: string[];
export const VERIFICATION_KEYS: string[];
export function notesDir(): string;
export function textRules(): { file: string; banned: RegExp[]; forbiddenKeys: string[]; mustCatch: string[] } | null;
export function provenanceProblems(value: unknown, where?: string): string[];
export function verificationProblems(v: unknown, where: string): string[];
export function queryListProblems(
  queries: Array<{ name: string; [k: string]: unknown }>,
  options?: { names?: string[]; label?: string },
): string[];
