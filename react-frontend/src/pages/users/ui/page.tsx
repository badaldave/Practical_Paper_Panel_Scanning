import React, { useEffect, useState } from 'react';
import { Plus, KeyRound, Pencil, Trash2 } from 'lucide-react';
import { usersApi, rolesApi, type Role, type SaveUserBody } from '@/shared/api/services';
import type { User } from '@/entities/user/model/store';
import { useAuthStore } from '@/entities/user/model/store';
import { PageHeader, Card, Button, Badge, Modal, Input, Select, Field, Spinner, EmptyState } from '@/shared/ui/primitives';

export const UsersPage: React.FC = () => {
  const canManage = useAuthStore((s) => s.hasPermission('users.manage'));
  const [users, setUsers] = useState<User[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [editing, setEditing] = useState<User | null>(null);
  const [creating, setCreating] = useState(false);
  const [pwUser, setPwUser] = useState<User | null>(null);

  const load = async () => {
    try {
      setError(null);
      const [u, r] = await Promise.all([usersApi.list(), rolesApi.list().catch(() => [] as Role[])]);
      setUsers(u);
      setRoles(r);
    } catch (err: any) {
      setError(err.message || 'Failed to load users');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const remove = async (u: User) => {
    if (!confirm(`Delete user ${u.email}? This cannot be undone.`)) return;
    try {
      await usersApi.remove(u.id);
      load();
    } catch (err: any) {
      alert(err.message);
    }
  };

  return (
    <>
      <PageHeader
        title="Users"
        subtitle="Manage who can access the platform and what they can do"
        actions={canManage && <Button onClick={() => setCreating(true)}><Plus size={15} /> New user</Button>}
      />
      <div className="p-6">
        {error && <div className="mb-4 rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{error}</div>}
        <Card>
          {loading ? (
            <Spinner label="Loading users…" />
          ) : users.length === 0 ? (
            <EmptyState title="No users yet" hint="Create your first user to get started." />
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b border-slate-800 text-xs uppercase tracking-wider text-slate-400">
                <tr>
                  <th className="px-4 py-3">Name</th>
                  <th className="px-4 py-3">Email</th>
                  <th className="px-4 py-3">Roles</th>
                  <th className="px-4 py-3">Status</th>
                  {canManage && <th className="px-4 py-3 text-right">Actions</th>}
                </tr>
              </thead>
              <tbody>
                {users.map((u) => (
                  <tr key={u.id} className="border-b border-slate-800/60 hover:bg-slate-900/40">
                    <td className="px-4 py-3 font-medium text-slate-200">{`${u.first_name} ${u.last_name}`.trim() || '—'}</td>
                    <td className="px-4 py-3 text-slate-400">{u.email}</td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1">
                        {u.roles?.length ? u.roles.map((r) => <Badge key={r} color="blue">{r}</Badge>) : <span className="text-xs text-slate-500">No role</span>}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge color={u.status === 'active' ? 'green' : u.status === 'suspended' ? 'red' : 'amber'}>{u.status}</Badge>
                    </td>
                    {canManage && (
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button variant="ghost" onClick={() => setEditing(u)} title="Edit"><Pencil size={14} /></Button>
                          <Button variant="ghost" onClick={() => setPwUser(u)} title="Reset password"><KeyRound size={14} /></Button>
                          <Button variant="ghost" onClick={() => remove(u)} title="Delete"><Trash2 size={14} /></Button>
                        </div>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </div>

      {(creating || editing) && (
        <UserFormModal
          user={editing}
          roles={roles}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
          onSaved={() => {
            setCreating(false);
            setEditing(null);
            load();
          }}
        />
      )}
      {pwUser && <ResetPasswordModal user={pwUser} onClose={() => setPwUser(null)} />}
    </>
  );
};

function UserFormModal({ user, roles, onClose, onSaved }: { user: User | null; roles: Role[]; onClose: () => void; onSaved: () => void }) {
  const isEdit = !!user;
  const [email, setEmail] = useState(user?.email ?? '');
  const [firstName, setFirstName] = useState(user?.first_name ?? '');
  const [lastName, setLastName] = useState(user?.last_name ?? '');
  const [password, setPassword] = useState('');
  const [status, setStatus] = useState(user?.status ?? 'active');
  const [roleIds, setRoleIds] = useState<string[]>(() => roles.filter((r) => user?.roles?.includes(r.name)).map((r) => r.id));
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const toggleRole = (id: string) => setRoleIds((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));

  const submit = async () => {
    setSaving(true);
    setErr(null);
    try {
      const body: SaveUserBody = { email, first_name: firstName, last_name: lastName, status, role_ids: roleIds };
      if (isEdit) {
        await usersApi.update(user!.id, body);
      } else {
        await usersApi.create({ ...body, password });
      }
      onSaved();
    } catch (e: any) {
      setErr(e.message || 'Save failed');
      setSaving(false);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? 'Edit user' : 'New user'}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button onClick={submit} disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
        </>
      }
    >
      <div className="space-y-3">
        {err && <div className="rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{err}</div>}
        <div className="grid grid-cols-2 gap-3">
          <Field label="First name"><Input value={firstName} onChange={(e) => setFirstName(e.target.value)} /></Field>
          <Field label="Last name"><Input value={lastName} onChange={(e) => setLastName(e.target.value)} /></Field>
        </div>
        <Field label="Email"><Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} /></Field>
        {!isEdit && (
          <Field label="Temporary password (min 8 chars)">
            <Input type="text" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        )}
        <Field label="Status">
          <Select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="active">Active</option>
            <option value="suspended">Suspended</option>
            <option value="pending">Pending</option>
          </Select>
        </Field>
        <Field label="Roles">
          <div className="grid grid-cols-2 gap-1.5">
            {roles.map((r) => (
              <label key={r.id} className="flex items-center gap-2 rounded-lg border border-slate-700 bg-slate-800 px-2.5 py-1.5 text-sm">
                <input type="checkbox" checked={roleIds.includes(r.id)} onChange={() => toggleRole(r.id)} />
                <span className="truncate">{r.name}</span>
              </label>
            ))}
            {roles.length === 0 && <span className="text-xs text-slate-500">No roles available to assign.</span>}
          </div>
        </Field>
      </div>
    </Modal>
  );
}

function ResetPasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const [pw, setPw] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const submit = async () => {
    setSaving(true);
    setErr(null);
    try {
      await usersApi.resetPassword(user.id, pw);
      setDone(true);
    } catch (e: any) {
      setErr(e.message || 'Reset failed');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={`Reset password — ${user.email}`}
      footer={
        done ? (
          <Button onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>Cancel</Button>
            <Button onClick={submit} disabled={saving}>{saving ? 'Saving…' : 'Reset password'}</Button>
          </>
        )
      }
    >
      {done ? (
        <p className="text-sm text-emerald-300">Password updated successfully.</p>
      ) : (
        <div className="space-y-3">
          {err && <div className="rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{err}</div>}
          <Field label="New password (min 8 chars)"><Input type="text" value={pw} onChange={(e) => setPw(e.target.value)} /></Field>
        </div>
      )}
    </Modal>
  );
}
