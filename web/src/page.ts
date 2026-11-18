import type { MessagePage } from './types';

/** Append a fetched page without repeating messages already shown. */
export function appendPage(current: MessagePage, next: MessagePage): MessagePage {
  const seen = new Set(current.messages.map((message) => message.id));
  const added = next.messages.filter((message) => !seen.has(message.id));
  return {
    messages: [...current.messages, ...added],
    nextCursor: next.nextCursor,
    hasMore: next.hasMore,
  };
}
