import { useCallback, useEffect, useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import { MailflowApi, RequestError } from './api';
import { appendPage } from './page';
import type {
  Attachment, Auth, Mailbox, Message, MessagePage, MessageSummary, PatchInput,
} from './types';
import './styles.css';

const EMPTY_PAGE: MessagePage = { messages: [], hasMore: false };

function describe(error: unknown): string {
  if (error instanceof RequestError) return `${error.code}: ${error.message}${error.replay ? ' (replayed)' : ''}`;
  return error instanceof Error ? error.message : 'Unexpected failure';
}

export default function App() {
  const [auth, setAuth] = useState<Auth | null>(null);
  const [user, setUser] = useState('');
  const [password, setPassword] = useState('');
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([]);
  const [mailboxId, setMailboxId] = useState('');
  const [page, setPage] = useState<MessagePage>(EMPTY_PAGE);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState('');
  const [message, setMessage] = useState<Message | null>(null);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [compose, setCompose] = useState(false);
  const [replyTo, setReplyTo] = useState<Message | null>(null);
  const [replyText, setReplyText] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const api = useMemo(() => (auth ? new MailflowApi(auth) : null), [auth]);

  const run = useCallback(async (work: () => Promise<void>) => {
    setError('');
    setNotice('');
    try {
      await work();
    } catch (err) {
      setError(describe(err));
    }
  }, []);

  const refreshMailboxes = useCallback(async (client: MailflowApi) => {
    const { data } = await client.mailboxes();
    setMailboxes(data.mailboxes);
    return data.mailboxes;
  }, []);

  useEffect(() => {
    if (!api) return;
    void run(async () => {
      const boxes = await refreshMailboxes(api);
      const inbox = boxes.find((box) => box.role === 'inbox');
      if (inbox) {
        setMailboxId(inbox.id);
        const { data } = await api.messages(inbox.id);
        setPage(data);
        setSearching(false);
      }
    });
  }, [api, refreshMailboxes, run]);

  function signIn(event: FormEvent) {
    event.preventDefault();
    setAuth({ user, password });
    setPassword('');
  }

  function signOut() {
    setAuth(null);
    setMailboxes([]);
    setMailboxId('');
    setPage(EMPTY_PAGE);
    setMessage(null);
    setAttachments([]);
  }

  function openMailbox(id: string) {
    if (!api) return;
    setMailboxId(id);
    setSearching(false);
    setMessage(null);
    void run(async () => {
      const { data } = await api.messages(id);
      setPage(data);
    });
  }

  function doSearch(event: FormEvent) {
    event.preventDefault();
    if (!api || !query.trim()) return;
    void run(async () => {
      const { data } = await api.search(query.trim(), mailboxId || undefined);
      setPage(data);
      setSearching(true);
      setMessage(null);
    });
  }

  function loadMore() {
    if (!api || !page.nextCursor) return;
    void run(async () => {
      const { data } = searching
        ? await api.search(query.trim(), mailboxId || undefined, undefined, page.nextCursor)
        : await api.messages(mailboxId, undefined, page.nextCursor);
      setPage(appendPage(page, data));
    });
  }

  function openMessage(summary: MessageSummary) {
    if (!api) return;
    void run(async () => {
      const [{ data: detail }, { data: files }] = await Promise.all([
        api.message(summary.id), api.attachments(summary.id),
      ]);
      setMessage(detail);
      setAttachments(files.attachments);
      if (!summary.read) {
        const { data: updated } = await api.patch(summary.id, { read: true });
        setMessage(updated);
      }
    });
  }

  function download(attachment: Attachment) {
    if (!api || !message) return;
    void run(async () => {
      const blob = await api.downloadAttachment(message.id, attachment.position);
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = attachment.filename;
      link.click();
      URL.revokeObjectURL(url);
    });
  }

  function refreshCurrent() {
    if (!api) return Promise.resolve();
    if (searching) return api.search(query.trim(), mailboxId || undefined).then(({ data }) => setPage(data));
    if (mailboxId) return api.messages(mailboxId).then(({ data }) => setPage(data));
    return Promise.resolve();
  }

  function sendCompose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!api) return;
    const form = new FormData(event.currentTarget);
    const to = String(form.get('to') ?? '').trim();
    const input = {
      to: to ? [{ name: '', email: to }] : [],
      cc: [],
      subject: String(form.get('subject') ?? ''),
      text: String(form.get('text') ?? ''),
    };
    void run(async () => {
      const { data, replay } = await api.send(input);
      setCompose(false);
      setNotice(`${replay ? 'Original result for' : 'Sent'} ${data.id}`);
      await refreshCurrent();
    });
  }

  function saveDraft() {
    if (!api) return;
    const element = document.getElementById('compose-form') as HTMLFormElement | null;
    if (!element) return;
    const form = new FormData(element);
    const to = String(form.get('to') ?? '').trim();
    void run(async () => {
      const { data } = await api.createDraft({
        to: to ? [{ name: '', email: to }] : [],
        cc: [],
        subject: String(form.get('subject') ?? ''),
        text: String(form.get('text') ?? ''),
      });
      setCompose(false);
      setNotice(`Saved draft ${data.id}`);
      await refreshMailboxes(api);
      await refreshCurrent();
    });
  }

  function submitDraft(id: string) {
    if (!api) return;
    void run(async () => {
      const { data, replay } = await api.sendDraft(id);
      setMessage(null);
      setNotice(`${replay ? 'Original result for' : 'Sent'} ${data.id}`);
      await refreshMailboxes(api);
      await refreshCurrent();
    });
  }

  function submitReply(event: FormEvent) {
    event.preventDefault();
    if (!api || !replyTo) return;
    void run(async () => {
      const { data } = await api.reply(replyTo.id, replyText);
      setReplyTo(null);
      setReplyText('');
      setNotice(`Saved reply draft ${data.id}`);
      await refreshMailboxes(api);
      await refreshCurrent();
    });
  }

  function patchCurrent(patch: PatchInput) {
    if (!api || !message) return;
    void run(async () => {
      const { data } = await api.patch(message.id, patch);
      setMessage(data);
      await refreshCurrent();
    });
  }

  function deleteCurrent() {
    if (!api || !message) return;
    void run(async () => {
      await api.remove(message.id);
      setMessage(null);
      setNotice(`Moved ${message.id} to trash`);
      await refreshMailboxes(api);
      await refreshCurrent();
    });
  }

  if (!auth) {
    return (
      <main className="signin">
        <h1>Mailflow</h1>
        <p>Sign in with your mail account to use the client.</p>
        <form onSubmit={signIn}>
          <label>Address<input value={user} onChange={(e) => setUser(e.target.value)} required autoFocus /></label>
          <label>Password<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></label>
          <button type="submit">Sign in</button>
        </form>
        <p className="hint">Credentials stay in memory for this session only.</p>
      </main>
    );
  }

  const drafts = mailboxes.find((box) => box.role === 'drafts');
  const isDraft = message && drafts?.id === mailboxId;

  return (
    <div className="app">
      <header>
        <h1>Mailflow</h1>
        <span className="account">{auth.user}</span>
        <button onClick={signOut}>Sign out</button>
      </header>
      {error && <div className="banner error" role="alert">{error}</div>}
      {notice && <div className="banner notice" role="status">{notice}</div>}
      <div className="layout">
        <aside>
          <h2>Mailboxes</h2>
          <ul>
            {mailboxes.map((box) => (
              <li key={box.id}>
                <button className={box.id === mailboxId && !searching ? 'active' : ''}
                  onClick={() => openMailbox(box.id)}>
                  {box.name} <span>{box.unread}/{box.total}</span>
                </button>
              </li>
            ))}
          </ul>
          <button className="primary" onClick={() => setCompose(true)}>Compose</button>
        </aside>
        <section className="list-pane">
          <form onSubmit={doSearch} className="search">
            <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search mail" />
            <button type="submit">Search</button>
          </form>
          <h2>{searching ? `Search: ${query}` : mailboxes.find((b) => b.id === mailboxId)?.name ?? 'Messages'}</h2>
          <ul className="messages">
            {page.messages.map((item) => (
              <li key={item.id}>
                <button className={message?.id === item.id ? 'active' : ''} onClick={() => openMessage(item)}>
                  <strong>{item.subject || '(no subject)'}</strong>
                  <span>{item.from.email}</span>
                  <time>{item.receivedAt.slice(0, 16).replace('T', ' ')}</time>
                </button>
              </li>
            ))}
          </ul>
          {page.hasMore && <button onClick={loadMore}>Load more</button>}
        </section>
        <section className="detail-pane">
          {!message && <p className="hint">Select a message to read it.</p>}
          {message && (
            <article>
              <h2>{message.subject || '(no subject)'}</h2>
              <p className="meta">From {message.from.email} to {message.to.map((a) => a.email).join(', ')}</p>
              <pre className="body">{message.text}</pre>
              {attachments.length > 0 && (
                <div>
                  <h3>Attachments</h3>
                  <ul>
                    {attachments.map((attachment) => (
                      <li key={attachment.position}>
                        <button onClick={() => download(attachment)}>
                          {attachment.filename} ({attachment.size} bytes)
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              <div className="actions">
                <button onClick={() => patchCurrent({ read: !message.read })}>
                  Mark {message.read ? 'unread' : 'read'}
                </button>
                <button onClick={() => patchCurrent({ starred: !message.starred })}>
                  {message.starred ? 'Unstar' : 'Star'}
                </button>
                <button onClick={() => setReplyTo(message)}>Reply</button>
                {isDraft && <button onClick={() => submitDraft(message.id)}>Send draft</button>}
                <button className="danger" onClick={deleteCurrent}>Delete</button>
              </div>
            </article>
          )}
        </section>
      </div>
      {compose && (
        <dialog open>
          <form id="compose-form" onSubmit={sendCompose}>
            <h2>New message</h2>
            <label>To<input name="to" type="email" required /></label>
            <label>Subject<input name="subject" /></label>
            <label>Text<textarea name="text" rows={8} required /></label>
            <div className="actions">
              <button type="submit" className="primary">Send</button>
              <button type="button" onClick={saveDraft}>Save draft</button>
              <button type="button" onClick={() => setCompose(false)}>Cancel</button>
            </div>
          </form>
        </dialog>
      )}
      {replyTo && (
        <dialog open>
          <form onSubmit={submitReply}>
            <h2>Reply to {replyTo.subject || '(no subject)'}</h2>
            <label>Text<textarea value={replyText} onChange={(e) => setReplyText(e.target.value)} rows={8} required /></label>
            <div className="actions">
              <button type="submit" className="primary">Save reply draft</button>
              <button type="button" onClick={() => setReplyTo(null)}>Cancel</button>
            </div>
          </form>
        </dialog>
      )}
    </div>
  );
}
