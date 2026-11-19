// Bounded, read-only load check for the published /v1 contract.
//
// Run through app/scripts/load.sh or the manual GitHub workflow. The wrapper
// validates VUS/DURATION and supplies credentials; this script never reads a
// file and never writes mail or application state.
import http from 'k6/http';
import { check, sleep } from 'k6';
import encoding from 'k6/encoding';

const BASE_URL = __ENV.BASE_URL || 'http://api:8080';
const VUS = Number(__ENV.VUS || 10);
const HOLD = __ENV.DURATION || '1m';
const PARAMS = {
  headers: {
    Authorization: `Basic ${encoding.b64encode(
      `${__ENV.MAILFLOW_USER}:${__ENV.MAILFLOW_PASSWORD}`, 'std')}`,
  },
};

export const options = {
  scenarios: {
    reads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: VUS },
        { duration: HOLD, target: VUS },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    checks: ['rate>0.99'],
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<1000'],
  },
};

// Find one readable mailbox and message once, so the hot loop needs no setup
// calls and depends on no fixed identifier.
export function setup() {
  const boxes = http.get(`${BASE_URL}/v1/mailboxes`, PARAMS);
  if (boxes.status !== 200 || !Array.isArray(boxes.json('mailboxes'))) {
    throw new Error(`mailbox setup failed with status ${boxes.status}`);
  }
  const inbox = boxes.json('mailboxes').find((box) => box.role === 'inbox');
  if (!inbox) throw new Error('the caller has no inbox');

  const listing = http.get(
    `${BASE_URL}/v1/mailboxes/${encodeURIComponent(inbox.id)}/messages?limit=5`,
    PARAMS,
  );
  const messages = listing.json('messages') || [];
  return { inboxId: inbox.id, messageId: messages.length ? messages[0].id : null };
}

export default function (data) {
  const mailboxes = http.get(`${BASE_URL}/v1/mailboxes`, PARAMS);
  check(mailboxes, {
    'mailboxes are listed': (r) => r.status === 200 && Array.isArray(r.json('mailboxes')),
  });

  const messages = http.get(
    `${BASE_URL}/v1/mailboxes/${encodeURIComponent(data.inboxId)}/messages?limit=25`,
    PARAMS,
  );
  check(messages, {
    'messages are listed': (r) => r.status === 200 && Array.isArray(r.json('messages')),
  });

  const search = http.get(`${BASE_URL}/v1/search?q=Mailflow&limit=25`, PARAMS);
  check(search, {
    'search is bounded': (r) => r.status === 200 && r.json('hasMore') !== undefined,
  });

  if (data.messageId) {
    const id = encodeURIComponent(data.messageId);
    const detail = http.get(`${BASE_URL}/v1/messages/${id}`, PARAMS);
    check(detail, {
      'message is read': (r) => r.status === 200 && typeof r.json('text') === 'string',
    });

    const attachments = http.get(`${BASE_URL}/v1/messages/${id}/attachments`, PARAMS);
    check(attachments, {
      'attachments are listed': (r) => r.status === 200 && Array.isArray(r.json('attachments')),
    });
  }

  sleep(1);
}
