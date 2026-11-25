export { getContext } from './getContext';
export type { CollectedContext, ContextInput } from './getContext';
export { readGlobalEnvironment } from './env';
export type {
  ContextEnvironment,
  LocationLike,
  NavigatorLike,
  UserAgentDataLike,
  WindowLike,
} from './env';
export { detectBrowser } from './browser';
export type { BrowserInfo } from './browser';
export { detectPlatform } from './platform';
export type { Platform } from './platform';
export { detectViewport } from './viewport';
export type { Viewport } from './viewport';
export {
  sanitizeUrl,
  isSensitiveKey,
  looksLikeSecretValue,
  DEFAULT_SENSITIVE_QUERY_PARAMS,
  MAX_URL_LENGTH,
  TRUNCATION_MARKER,
} from './sanitizeUrl';
export type { SanitizeUrlOptions } from './sanitizeUrl';
