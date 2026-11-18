import { describe, expect, it, vi } from 'vitest';
import { basicAuth, MailflowApi, queryString } from './api';

function response(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

const auth = { user: 'bob@mailflow.test', password: 's3cret' };

describe('basicAuth', () => {
  it('encodes both parts without putting them in a URL', () => {
    expect(basicAuth(auth.user, auth.password)).toBe('Basic Ym9iQG1haWxmbG93LnRlc3Q6czNjcmV0');
  });
});

describe('queryString', () => {
  it('omits empty values and encodes text', () => {
    const result = queryString({ q: 'Re: lunch & coffee', mailboxId: 'mb1_a', limit: undefined });
    expect(result).toBe('?q=Re%3A+lunch+%26+coffee&mailboxId=mb1_a');
  });
});

describe('MailflowApi', () => {
  it('sends credentials in the header for a read', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ mailboxes: [] }));
    const api = new MailflowApi(auth, '', () => 'key', fetcher);
    await api.mailboxes();
    expect(fetcher.mock.calls[0][0]).toBe('/v1/mailboxes');
    expect(fetcher.mock.calls[0][1].headers.get('Authorization')).toBe(basicAuth(auth.user, auth.password));
  });

  it('builds a search request with only contract parameters', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ messages: [], hasMore: false }));
    const api = new MailflowApi(auth, '', () => 'key', fetcher);
    await api.search('hello there', 'mb1_a', 5);
    expect(fetcher.mock.calls[0][0]).toBe('/v1/search?q=hello+there&mailboxId=mb1_a&limit=5');
  });

  it('uses a fresh idempotency key for each send', async () => {
    const keys = ['one', 'two'];
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response({ id: 'msg1_a' }))
      .mockResolvedValueOnce(response({ id: 'msg1_b' }));
    const api = new MailflowApi(auth, '', () => keys.shift()!, fetcher);
    const input = { to: [{ name: '', email: 'alice@mailflow.test' }], subject: 'Hi', text: 'Hello' };
    await api.send(input);
    await api.send(input);
    expect(fetcher.mock.calls[0][1].headers.get('Idempotency-Key')).toBe('one');
    expect(fetcher.mock.calls[1][1].headers.get('Idempotency-Key')).toBe('two');
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual(input);
  });

  it('reports a replay without hiding the original result', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      response({ id: 'msg1_a' }, 201, { 'Idempotency-Replayed': 'true' }),
    );
    const api = new MailflowApi(auth, '', () => 'key', fetcher);
    const result = await api.sendDraft('msg1_a');
    expect(result).toEqual({ data: { id: 'msg1_a' }, replay: true });
  });

  it('maps a contract error to its code and message', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      response({ code: 'not_found', message: 'not found' }, 404),
    );
    const api = new MailflowApi(auth, '', () => 'key', fetcher);
    await expect(api.message('msg1_missing')).rejects.toMatchObject({
      name: 'RequestError', code: 'not_found', status: 404, message: 'not found',
    });
  });

  it('submits a draft with its identifier and a key', async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ id: 'msg1_draft' }, 201));
    const api = new MailflowApi(auth, '', () => 'key-1', fetcher);
    await api.sendDraft('msg1_draft');
    expect(fetcher.mock.calls[0][0]).toBe('/v1/drafts/msg1_draft/send');
    expect(fetcher.mock.calls[0][1].method).toBe('POST');
    expect(fetcher.mock.calls[0][1].headers.get('Idempotency-Key')).toBe('key-1');
  });

  it('downloads attachment bytes with credentials, not a JSON body', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response('bytes'));
    const api = new MailflowApi(auth, '', () => 'key', fetcher);
    const blob = await api.downloadAttachment('msg1_a', 0);
    expect(await blob.text()).toBe('bytes');
    expect(fetcher.mock.calls[0][0]).toBe('/v1/messages/msg1_a/attachments/0');
    expect(fetcher.mock.calls[0][1].body).toBeUndefined();
    expect(fetcher.mock.calls[0][1].headers.get('Authorization')).toBe(basicAuth(auth.user, auth.password));
  });
});
