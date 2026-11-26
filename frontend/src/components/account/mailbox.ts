import { adminAPI } from '@/api/admin'
import type { MailboxConfig } from '@/api/admin/accounts'
import type { Account } from '@/types'

/** Credential keys an account's mailbox config is stored under (imap_password is redacted server-side). */
export const MAILBOX_CREDENTIAL_KEYS = {
  host: 'imap_host',
  port: 'imap_port',
  username: 'imap_username',
  password: 'imap_password',
  folder: 'imap_folder'
} as const

// Earlier builds remembered the last mailbox (including its password) in
// localStorage and pre-filled it for every new account. That is no longer done;
// remove any copy left behind in this browser.
const LEGACY_STORAGE_KEY = 'sub2api.mailbox.v1'
try {
  localStorage.removeItem(LEGACY_STORAGE_KEY)
} catch {
  // storage unavailable — ignore
}

export function defaultMailboxConfig(): MailboxConfig {
  return { host: 'imap.gmail.com', port: 993, username: '', password: '', folder: 'INBOX' }
}

/** Mailbox config saved on an account (password is never returned; see hasSavedPassword). */
export function mailboxFromAccount(account: Account | null | undefined): MailboxConfig | null {
  const creds = (account?.credentials ?? {}) as Record<string, unknown>
  const username = typeof creds.imap_username === 'string' ? creds.imap_username : ''
  if (!username) return null
  return {
    host: typeof creds.imap_host === 'string' && creds.imap_host ? creds.imap_host : 'imap.gmail.com',
    port: Number(creds.imap_port) || 993,
    username,
    password: '',
    folder: typeof creds.imap_folder === 'string' && creds.imap_folder ? creds.imap_folder : 'INBOX'
  }
}

export function accountHasSavedMailboxPassword(account: Account | null | undefined): boolean {
  return !!account?.credentials_status?.has_imap_password
}

export function isMailboxComplete(config: MailboxConfig | null | undefined): config is MailboxConfig {
  return !!config && !!config.host.trim() && !!config.username.trim() && !!config.password
}

export function mailboxCredentials(config: MailboxConfig, includePassword = true): Record<string, unknown> {
  const out: Record<string, unknown> = {
    [MAILBOX_CREDENTIAL_KEYS.host]: config.host.trim(),
    [MAILBOX_CREDENTIAL_KEYS.port]: config.port || 993,
    [MAILBOX_CREDENTIAL_KEYS.username]: config.username.trim(),
    [MAILBOX_CREDENTIAL_KEYS.folder]: config.folder.trim() || 'INBOX'
  }
  if (includePassword && config.password) out[MAILBOX_CREDENTIAL_KEYS.password] = config.password
  return out
}

/** Merge a complete mailbox config into a create-account payload's credentials. */
export function withMailboxCredentials<T extends { credentials?: Record<string, unknown> }>(
  payload: T,
  config: MailboxConfig | null | undefined
): T {
  if (!isMailboxComplete(config)) return payload
  return { ...payload, credentials: { ...(payload.credentials ?? {}), ...mailboxCredentials(config) } }
}

/**
 * Save mailbox config onto an existing account. The backend keeps sensitive keys
 * (including imap_password) when they are omitted, so a blank password keeps the saved one.
 */
export async function saveAccountMailbox(accountId: number, config: MailboxConfig): Promise<Account> {
  const fresh = await adminAPI.accounts.getById(accountId)
  const credentials = {
    ...((fresh.credentials ?? {}) as Record<string, unknown>),
    ...mailboxCredentials(config)
  }
  return adminAPI.accounts.update(accountId, { credentials })
}
