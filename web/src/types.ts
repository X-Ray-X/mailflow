// The client's view of the published contract. These are deliberately the
// Mailflow fields only; no mail server type is represented here.

export interface Address {
  name: string;
  email: string;
}

export interface Mailbox {
  id: string;
  name: string;
  role: string;
  unread: number;
  total: number;
}

export interface MessageSummary {
  id: string;
  subject: string;
  from: Address;
  receivedAt: string;
  read: boolean;
  starred: boolean;
  hasAttachment: boolean;
}

export interface Message extends MessageSummary {
  to: Address[];
  cc: Address[];
  text: string;
  textTruncated: boolean;
}

export interface Attachment {
  position: number;
  filename: string;
  contentType: string;
  size: number;
}

export interface MessagePage {
  messages: MessageSummary[];
  nextCursor?: string;
  hasMore: boolean;
}

export interface Auth {
  user: string;
  password: string;
}

export interface SendInput {
  to: Address[];
  cc?: Address[];
  subject: string;
  text: string;
}

export interface DraftInput extends SendInput {}

export interface PatchInput {
  read?: boolean;
  starred?: boolean;
  mailboxId?: string;
}
