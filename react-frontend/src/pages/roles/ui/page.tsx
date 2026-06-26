import React, { useEffect, useMemo, useState } from 'react';
import { Plus, Pencil, Trash2, Lock } from 'lucide-react';
import { rolesApi, type Role, type Permission } from '@/shared/api/services';
import { useAuthStore } from '@/entities/user/model/store';
import { PageHeader, Card, Button, Badge, Modal, Input, Field, Spinner } from '@/shared/ui/primitives';

export const RolesPage: React.FC = () => {
  const canManage = useAuthStore((s) => s.hasPermission('roles.manage'));
  const [roles, setRoles] = useState<Role[]>([]);
  const [perms, setPerms] = useState<Permission[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<Role | null>(null);
  const [creating, setCreating] = useState(false);

  const load = async () => {
    try {
      setError(null);
      const [r, p] = await Promise.all([rolesApi.list(), rolesApi.permissions()]);
      setRoles(r);
      setPerms(p);
    } catch (err: any) {
      setError(err.message || 'Failed to load roles');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const remove = async (r: Role) => {
    if (!confirm(`Delete role "${r.name}"?`)) return;
    try {
      await rolesApi.remove(r.id);
      load();
    } catch (err: any) {
      alert(err.message);
    }
  };

  return (
    <>
      <PageHeader
        title="Roles & Permissions"
        subtitle="Build roles and control exactly what each can do"
        actions={canManage && <Button onClick={() => setCreating(true)}><Plus size={15} /> New role</Button>}
      />
      <div className="p-6">
        {error && <div className="mb-4 rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{error}</div>}
        {loading ? (
          <Spinner label="Loading roles…" />
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {roles.map((r) => (
              <Card key={r.id} className="flex flex-col p-4">
                <div className="flex items-start justify-between">
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="font-semibold text-white">{r.name}</h3>
                      {r.is_system ? <Badge color="purple"><Lock size={10} className="mr-1 inline" />System</Badge> : <Badge color="blue">Custom</Badge>}
                    </div>
                    <p className="mt-1 text-xs text-slate-400">{r.description || 'No description'}</p>
                  </div>
                </div>
                <div className="mt-3 flex items-center gap-3 text-xs text-slate-400">
                  <span>{r.permissions?.length ?? 0} permissions</span>
                  <span>·</span>
                  <span>{r.user_count} users</span>
                </div>
                <div className="mt-4 flex gap-2">
                  <Button variant="ghost" className="flex-1" onClick={() => setEditing(r)}>
                    <Pencil size={14} /> {r.is_system || !canManage ? 'View' : 'Edit'}
                  </Button>
                  {canManage && !r.is_system && (
                    <Button variant="ghost" onClick={() => remove(r)} title="Delete"><Trash2 size={14} /></Button>
                  )}
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>

      {(creating || editing) && (
        <RoleFormModal
          role={editing}
          perms={perms}
          readOnly={!!editing?.is_system || !canManage}
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
    </>
  );
};

function RoleFormModal({
  role,
  perms,
  readOnly,
  onClose,
  onSaved,
}: {
  role: Role | null;
  perms: Permission[];
  readOnly: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEdit = !!role;
  const [name, setName] = useState(role?.name ?? '');
  const [description, setDescription] = useState(role?.description ?? '');
  const [selected, setSelected] = useState<string[]>(role?.permissions ?? []);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const grouped = useMemo(() => {
    const g: Record<string, Permission[]> = {};
    for (const p of perms) (g[p.category] ??= []).push(p);
    return g;
  }, [perms]);

  const toggle = (code: string) => {
    if (readOnly) return;
    setSelected((p) => (p.includes(code) ? p.filter((x) => x !== code) : [...p, code]));
  };
  const toggleCategory = (codes: string[], all: boolean) => {
    if (readOnly) return;
    setSelected((p) => (all ? p.filter((c) => !codes.includes(c)) : Array.from(new Set([...p, ...codes]))));
  };

  const submit = async () => {
    setSaving(true);
    setErr(null);
    try {
      const body = { name, description, permissions: selected };
      if (isEdit) await rolesApi.update(role!.id, body);
      else await rolesApi.create(body);
      onSaved();
    } catch (e: any) {
      setErr(e.message || 'Save failed');
      setSaving(false);
    }
  };

  return (
    <Modal
      open
      wide
      onClose={onClose}
      title={readOnly ? `Role — ${role?.name}` : isEdit ? 'Edit role' : 'New role'}
      footer={
        readOnly ? (
          <Button onClick={onClose}>Close</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>Cancel</Button>
            <Button onClick={submit} disabled={saving}>{saving ? 'Saving…' : 'Save role'}</Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        {err && <div className="rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{err}</div>}
        {readOnly && <div className="rounded-lg bg-slate-800 px-3 py-2 text-xs text-slate-300">This is a built-in system role and cannot be edited.</div>}
        <div className="grid grid-cols-2 gap-3">
          <Field label="Role name"><Input value={name} disabled={readOnly} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="Description"><Input value={description} disabled={readOnly} onChange={(e) => setDescription(e.target.value)} /></Field>
        </div>

        <div>
          <div className="mb-2 text-xs font-medium text-slate-400">Permissions</div>
          <div className="space-y-3">
            {Object.entries(grouped).map(([cat, list]) => {
              const codes = list.map((p) => p.code);
              const allOn = codes.every((c) => selected.includes(c));
              return (
                <div key={cat} className="rounded-lg border border-slate-800 p-3">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-sm font-semibold capitalize text-slate-200">{cat}</span>
                    {!readOnly && (
                      <button onClick={() => toggleCategory(codes, allOn)} className="text-xs text-blue-400 hover:underline">
                        {allOn ? 'Clear all' : 'Select all'}
                      </button>
                    )}
                  </div>
                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                    {list.map((p) => (
                      <label key={p.code} className={`flex items-center gap-2 rounded-md px-2 py-1.5 text-sm ${readOnly ? 'opacity-80' : 'hover:bg-slate-800'}`}>
                        <input type="checkbox" checked={selected.includes(p.code)} disabled={readOnly} onChange={() => toggle(p.code)} />
                        <span className="text-slate-300" title={p.description}>{p.code}</span>
                      </label>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </Modal>
  );
}
