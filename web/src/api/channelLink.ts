import { teamFetch } from './teamContext'

export type LinkChannel = 'telegram' | 'slack'
export interface LinkIdentity { user_id: string; workspace_id?: string }
export interface LinkCodeResponse { code: string; expires_in: number; bot_username?: string; bot_user_id?: string }
export interface LinkedChannel {
  id: string; user_id: string; channel_type: string; external_id: string
  display_name: string; enabled: boolean; verified: boolean; verified_at?: string; created_at: string
}
export function linkPrincipal(channel: LinkChannel, identity: LinkIdentity): string | null {
  if (channel === 'telegram') {
    if (!/^[1-9][0-9]{0,18}$/.test(identity.user_id) || identity.workspace_id) return null
    if (BigInt(identity.user_id) > 9223372036854775807n) return null
    return identity.user_id
  }
  return /^T[A-Z0-9]{2,63}$/.test(identity.workspace_id || '') && /^[UW][A-Z0-9]{2,63}$/.test(identity.user_id)
    ? `${identity.workspace_id}:${identity.user_id}` : null
}
export const validEmailLinkCode = (code: string): boolean => /^[A-Za-z0-9_-]{22}$/.test(code.trim())

export async function generateChannelLinkCode(channel: LinkChannel, identity: LinkIdentity): Promise<LinkCodeResponse> {
  if (!linkPrincipal(channel, identity)) throw new Error(`Enter a valid expected ${channel} identity before generating a code.`)
  const response = await teamFetch('/api/user/channels/link-code', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include',
    body: JSON.stringify({ channel_type: channel, expected_identity: identity }),
  })
  if (!response.ok) {
    const data = await response.json().catch(() => ({})) as { error?: string }
    throw new Error(data.error || 'Failed to generate link code')
  }
  return response.json() as Promise<LinkCodeResponse>
}
export async function listLinkedChannels(): Promise<LinkedChannel[]> {
  const response = await teamFetch('/api/user/channels', { credentials: 'include' })
  if (!response.ok) throw new Error('Failed to load linked channels')
  const data = await response.json() as { channels?: LinkedChannel[] }
  return data.channels || []
}
