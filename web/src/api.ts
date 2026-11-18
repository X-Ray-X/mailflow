import type {
  Attachment, Auth, DraftInput, Mailbox, Message, MessagePage, PatchInput, SendInput,
} from './types';

/** The contract's error body, surfaced to the UI rather than swallowed. */
export class RequestError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
    public readonly replay = false,
  ) {
    super(message);
    this.name = 'RequestError';
  }
}

export interface Result<T> {
  data: T;
  replay: boolean;
}

/** Basic auth without placing credentials in a URL or browser storage. */
export function basicAuth(user: string, password: string): string {
  const bytes = new TextEncoder().encode(`${user}:${password}`);
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return `Basic ${btoa(binary)}`;
}

export function queryString(params: Record<string, string | number | undefined>): string {
  const query = new URLSearchParams();
  for (const [name, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(name, String(value));
  }
  const encoded = query.toString();
  return encoded ? `?${encoded}` : '';
}

type Fetcher = (input: string, init?: RequestInit) => Promise<Response>;

/** Every mail request goes through this client and the published /v1 API. */
export class MailflowApi {
  constructor(
    private readonly auth: Auth,
    private readonly base = '',
    private readonly newKey: () => string = () => crypto.randomUUID(),
    private readonly fetcher: Fetcher = fetch,
  ) {}

  private headers(extra: HeadersInit = {}): Headers {
    const headers = new Headers(extra);
    headers.set('Authorization', basicAuth(this.auth.user, this.auth.password));
    return headers;
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<Result<T>> {
    const headers = this.headers(init.headers);
    if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const response = await this.fetcher(`${this.base}${path}`, { ...init, headers });
    const raw = await response.text();
    const data = raw ? JSON.parse(raw) : {};
    const replay = response.headers.get('Idempotency-Replayed') === 'true';
    if (!response.ok) {
      throw new RequestError(data.code ?? 'internal', data.message ?? response.statusText, response.status, replay);
    }
    return { data: data as T, replay };
  }

  mailboxes(): Promise<Result<{ mailboxes: Mailbox[] }>> {
    return this.request('/v1/mailboxes');
  }

  messages(mailboxId: string, limit?: number, cursor?: string): Promise<Result<MessagePage>> {
    return this.request(`/v1/mailboxes/${encodeURIComponent(mailboxId)}/messages${queryString({ limit, cursor })}`);
  }

  search(text: string, mailboxId?: string, limit?: number, cursor?: string): Promise<Result<MessagePage>> {
    return this.request(`/v1/search${queryString({ q: text, mailboxId, limit, cursor })}`);
  }

  message(id: string): Promise<Result<Message>> {
    return this.request(`/v1/messages/${encodeURIComponent(id)}`);
  }

  attachments(id: string): Promise<Result<{ attachments: Attachment[] }>> {
    return this.request(`/v1/messages/${encodeURIComponent(id)}/attachments`);
  }

  async downloadAttachment(id: string, position: number): Promise<Blob> {
    const response = await this.fetcher(
      `${this.base}/v1/messages/${encodeURIComponent(id)}/attachments/${position}`,
      { headers: this.headers() },
    );
    if (!response.ok) {
      const raw = await response.text();
      const data = raw ? JSON.parse(raw) : {};
      throw new RequestError(data.code ?? 'internal', data.message ?? response.statusText, response.status);
    }
    return response.blob();
  }

  send(input: SendInput): Promise<Result<{ id: string }>> {
    return this.request('/v1/messages', {
      method: 'POST',
      body: JSON.stringify(input),
      headers: { 'Idempotency-Key': this.newKey() },
    });
  }

  createDraft(input: DraftInput): Promise<Result<{ id: string }>> {
    return this.request('/v1/drafts', { method: 'POST', body: JSON.stringify(input) });
  }

  sendDraft(id: string): Promise<Result<{ id: string }>> {
    return this.request(`/v1/drafts/${encodeURIComponent(id)}/send`, {
      method: 'POST',
      headers: { 'Idempotency-Key': this.newKey() },
    });
  }

  reply(id: string, text: string): Promise<Result<{ id: string }>> {
    return this.request(`/v1/messages/${encodeURIComponent(id)}/reply`, {
      method: 'POST',
      body: JSON.stringify({ text }),
    });
  }

  patch(id: string, patch: PatchInput): Promise<Result<Message>> {
    return this.request(`/v1/messages/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    });
  }

  remove(id: string): Promise<Result<undefined>> {
    return this.request(`/v1/messages/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }
}
