import { describe, expect, it } from 'vitest';
import { appendPage } from './page';
import type { MessagePage, MessageSummary } from './types';

function summary(id: string): MessageSummary {
  return {
    id, subject: id, from: { name: '', email: 'alice@mailflow.test' },
    receivedAt: '2026-11-18T12:00:00Z', read: false, starred: false, hasAttachment: false,
  };
}

const first: MessagePage = { messages: [summary('a'), summary('b')], nextCursor: 'cur1_b', hasMore: true };

describe('appendPage', () => {
  it('adds new messages and keeps the next cursor', () => {
    const result = appendPage(first, { messages: [summary('c')], nextCursor: 'cur1_c', hasMore: false });
    expect(result.messages.map((m) => m.id)).toEqual(['a', 'b', 'c']);
    expect(result.nextCursor).toBe('cur1_c');
    expect(result.hasMore).toBe(false);
  });

  it('drops a repeated message from an overlapping page', () => {
    const result = appendPage(first, { messages: [summary('b'), summary('c')], hasMore: false });
    expect(result.messages.map((m) => m.id)).toEqual(['a', 'b', 'c']);
  });
});
