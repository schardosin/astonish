import React, { useState, useEffect, useCallback } from 'react'
import { Users, Plus, Trash2, Shield, UserPlus, ChevronRight, AlertCircle, Loader2 } from 'lucide-react'
import {
  fetchTeams, createTeam, deleteTeam,
  fetchTeamMembers, addTeamMember, removeTeamMember, setTeamMemberRole,
  type Team, type TeamMember,
} from '../api/platform'

interface TeamManagementProps {
  theme: 'dark' | 'light'
  user: { id: string; email: string; display_name: string; role: string }
  org: { id: string; name: string; slug: string }
  activeTeam: string | null
}

const slugify = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
const errMsg = (err: unknown, fallback: string) => err instanceof Error ? err.message : fallback
const gradientPurple = { background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }
const inputStyle = { background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }
const errorBg = { background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }

function InlineError({ msg }: { msg: string }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={errorBg}>
      <AlertCircle size={14} /><span>{msg}</span>
    </div>
  )
}

export default function TeamManagement({ user, activeTeam }: TeamManagementProps) {
  const [teams, setTeams] = useState<Team[]>([])
  const [selectedSlug, setSelectedSlug] = useState<string | null>(activeTeam)
  const [members, setMembers] = useState<TeamMember[]>([])
  const [teamsLoading, setTeamsLoading] = useState(true)
  const [membersLoading, setMembersLoading] = useState(false)
  const [teamsError, setTeamsError] = useState('')
  const [membersError, setMembersError] = useState('')
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newSlug, setNewSlug] = useState('')
  const [newDesc, setNewDesc] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [showAddMember, setShowAddMember] = useState(false)
  const [addEmail, setAddEmail] = useState('')
  const [addRole, setAddRole] = useState('member')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState('')

  const isAdmin = user.role === 'admin' || user.role === 'owner'
  const selectedTeam = teams.find(t => t.slug === selectedSlug) || null

  const loadTeams = useCallback(async () => {
    setTeamsLoading(true); setTeamsError('')
    try {
      const data = await fetchTeams()
      setTeams(data)
      if (!selectedSlug && data.length > 0) setSelectedSlug(activeTeam || data[0].slug)
    } catch (err) { setTeamsError(errMsg(err, 'Failed to load teams')) }
    finally { setTeamsLoading(false) }
  }, [selectedSlug, activeTeam])

  const loadMembers = useCallback(async (slug: string) => {
    setMembersLoading(true); setMembersError('')
    try { setMembers(await fetchTeamMembers(slug)) }
    catch (err) { setMembersError(errMsg(err, 'Failed to load members')) }
    finally { setMembersLoading(false) }
  }, [])

  // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch on mount/change
  useEffect(() => { void loadTeams() }, [loadTeams])
  // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch on selection change
  useEffect(() => { if (selectedSlug) void loadMembers(selectedSlug) }, [selectedSlug, loadMembers])

  const handleCreateTeam = async (e: React.FormEvent) => {
    e.preventDefault(); setCreating(true); setCreateError('')
    try {
      await createTeam(newName, newSlug, newDesc)
      setShowCreateModal(false); setNewName(''); setNewSlug(''); setNewDesc('')
      await loadTeams()
    } catch (err) { setCreateError(errMsg(err, 'Failed to create team')) }
    finally { setCreating(false) }
  }

  const handleDeleteTeam = async () => {
    if (!selectedSlug || selectedSlug === 'general') return
    try { await deleteTeam(selectedSlug); setSelectedSlug(null); await loadTeams() }
    catch (err) { setMembersError(errMsg(err, 'Failed to delete team')) }
  }

  const handleAddMember = async (e: React.FormEvent) => {
    e.preventDefault(); if (!selectedSlug) return
    setAdding(true); setAddError('')
    try {
      await addTeamMember(selectedSlug, addEmail, addRole)
      setShowAddMember(false); setAddEmail(''); setAddRole('member')
      await loadMembers(selectedSlug)
    } catch (err) { setAddError(errMsg(err, 'Failed to add member')) }
    finally { setAdding(false) }
  }

  const handleRemoveMember = async (userId: string) => {
    if (!selectedSlug) return
    try { await removeTeamMember(selectedSlug, userId); await loadMembers(selectedSlug) }
    catch (err) { setMembersError(errMsg(err, 'Failed to remove member')) }
  }

  const handleRoleChange = async (userId: string, role: string) => {
    if (!selectedSlug) return
    try { await setTeamMemberRole(selectedSlug, userId, role); await loadMembers(selectedSlug) }
    catch (err) { setMembersError(errMsg(err, 'Failed to update role')) }
  }

  return (
    <div className="flex h-full" style={{ background: 'var(--bg-primary)', color: 'var(--text-primary)' }}>
      {/* Left panel — Team list */}
      <div className="w-72 flex-shrink-0 flex flex-col border-r" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        <div className="flex items-center justify-between p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <div className="flex items-center gap-2">
            <Users size={18} style={{ color: 'var(--accent)' }} />
            <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>Teams</span>
          </div>
          {isAdmin && (
            <button onClick={() => setShowCreateModal(true)} className="p-1.5 rounded-lg transition-colors hover:opacity-80" style={{ background: 'var(--accent-soft)', color: 'var(--accent)' }}>
              <Plus size={16} />
            </button>
          )}
        </div>
        <div className="flex-1 overflow-y-auto p-2">
          {teamsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
            </div>
          ) : teamsError ? (
            <div className="flex items-center gap-2 p-3 text-sm" style={{ color: 'var(--danger)' }}>
              <AlertCircle size={14} /><span>{teamsError}</span>
            </div>
          ) : teams.map(team => (
            <button
              key={team.slug} onClick={() => setSelectedSlug(team.slug)}
              className="w-full flex items-center justify-between px-3 py-2.5 rounded-lg text-left text-sm transition-colors mb-0.5"
              style={{ background: selectedSlug === team.slug ? 'var(--accent-soft)' : 'transparent', color: selectedSlug === team.slug ? 'var(--accent)' : 'var(--text-secondary)' }}
            >
              <div className="flex items-center gap-2 truncate">
                <Shield size={14} /><span className="truncate">{team.name}</span>
              </div>
              {selectedSlug === team.slug && <ChevronRight size={14} />}
            </button>
          ))}
        </div>
      </div>

      {/* Right panel — Team detail */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {selectedTeam ? (
          <>
            <div className="flex items-center justify-between px-6 py-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
              <div>
                <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>{selectedTeam.name}</h2>
                {selectedTeam.description && <p className="text-sm mt-0.5" style={{ color: 'var(--text-muted)' }}>{selectedTeam.description}</p>}
              </div>
              {isAdmin && (
                <button onClick={() => setShowAddMember(true)} className="flex items-center gap-2 px-4 py-2 rounded-xl text-white text-sm font-medium hover:opacity-90" style={gradientPurple}>
                  <UserPlus size={16} />Add Member
                </button>
              )}
            </div>

            {showAddMember && (
              <form onSubmit={handleAddMember} className="flex items-center gap-3 px-6 py-3 border-b" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
                <input type="email" value={addEmail} onChange={e => setAddEmail(e.target.value)} placeholder="Email address" required className="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle} autoFocus />
                <select value={addRole} onChange={e => setAddRole(e.target.value)} className="px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle}>
                  <option value="member">Member</option>
                  <option value="admin">Admin</option>
                </select>
                <button type="submit" disabled={adding} className="px-4 py-2 rounded-lg text-white text-sm font-medium hover:opacity-90 disabled:opacity-50" style={gradientPurple}>
                  {adding ? <Loader2 size={16} className="animate-spin" /> : 'Add'}
                </button>
                <button type="button" onClick={() => { setShowAddMember(false); setAddError('') }} className="px-3 py-2 rounded-lg text-sm" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
                {addError && <span className="text-xs flex items-center gap-1" style={{ color: 'var(--danger)' }}><AlertCircle size={12} />{addError}</span>}
              </form>
            )}

            {membersError && <div className="mx-6 mt-3"><InlineError msg={membersError} /></div>}

            <div className="flex-1 overflow-y-auto px-6 py-4">
              {membersLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
                </div>
              ) : members.length === 0 ? (
                <p className="text-center py-12 text-sm" style={{ color: 'var(--text-muted)' }}>No members in this team.</p>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr style={{ color: 'var(--text-muted)' }}>
                      <th className="text-left py-2 px-3 font-medium">Email</th>
                      <th className="text-left py-2 px-3 font-medium">Name</th>
                      <th className="text-left py-2 px-3 font-medium">Role</th>
                      <th className="text-left py-2 px-3 font-medium">Joined</th>
                      {isAdmin && <th className="w-10" />}
                    </tr>
                  </thead>
                  <tbody>
                    {members.map(m => (
                      <tr key={m.user_id} className="border-t" style={{ borderColor: 'var(--border-color)' }}>
                        <td className="py-2.5 px-3" style={{ color: 'var(--text-primary)' }}>{m.email}</td>
                        <td className="py-2.5 px-3" style={{ color: 'var(--text-secondary)' }}>{m.display_name}</td>
                        <td className="py-2.5 px-3">
                          {isAdmin ? (
                            <select value={m.role} onChange={e => handleRoleChange(m.user_id, e.target.value)} className="px-2 py-1 rounded text-xs outline-none" style={inputStyle}>
                              <option value="member">member</option>
                              <option value="admin">admin</option>
                            </select>
                          ) : (
                            <span className="px-2 py-0.5 rounded text-xs" style={{ background: 'var(--accent-soft)', color: 'var(--accent)' }}>{m.role}</span>
                          )}
                        </td>
                        <td className="py-2.5 px-3" style={{ color: 'var(--text-muted)' }}>{new Date(m.joined_at).toLocaleDateString()}</td>
                        {isAdmin && (
                          <td className="py-2.5 px-3">
                            {m.user_id !== user.id && (
                              <button onClick={() => handleRemoveMember(m.user_id)} className="p-1 rounded hover:opacity-80 transition-opacity" style={{ color: 'var(--danger)' }} title="Remove member">
                                <Trash2 size={14} />
                              </button>
                            )}
                          </td>
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>

            {isAdmin && (
              <div className="px-6 py-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
                <button onClick={handleDeleteTeam} disabled={selectedSlug === 'general'} className="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium hover:opacity-90 disabled:opacity-30 disabled:cursor-not-allowed" style={{ background: 'var(--danger)', color: '#fff' }}>
                  <Trash2 size={14} />Delete Team
                </button>
              </div>
            )}
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Select a team to manage</p>
          </div>
        )}
      </div>

      {/* Create team modal */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setShowCreateModal(false)} />
          <div className="relative w-full max-w-md mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
            <div className="px-6 py-5" style={gradientPurple}>
              <h2 className="text-lg font-semibold text-white">Create Team</h2>
              <p className="text-sm text-white/70 mt-0.5">Add a new team to your organization</p>
            </div>
            <form onSubmit={handleCreateTeam} className="p-6 space-y-4">
              <InlineError msg={createError} />
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Name</label>
                <input type="text" value={newName} onChange={e => { setNewName(e.target.value); setNewSlug(slugify(e.target.value)) }} placeholder="Engineering" required className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Slug</label>
                <input type="text" value={newSlug} onChange={e => setNewSlug(e.target.value)} placeholder="engineering" required className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Description</label>
                <input type="text" value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="Optional description" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
              </div>
              <div className="flex gap-3 pt-2">
                <button type="button" onClick={() => { setShowCreateModal(false); setCreateError('') }} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
                <button type="submit" disabled={creating} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={gradientPurple}>
                  {creating ? <Loader2 size={16} className="animate-spin" /> : 'Create Team'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
