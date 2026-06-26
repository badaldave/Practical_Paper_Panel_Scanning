import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Lock, Play, Unlock, RefreshCw, UserCog } from 'lucide-react';
import { verificationApi, usersApi, type VerificationItem, type QueueScope } from '@/shared/api/services';
import type { User } from '@/entities/user/model/store';
import { useAuthStore } from '@/entities/user/model/store';
import { PageHeader, Card, Button, Badge, Select, Spinner, EmptyState, timeAgo } from '@/shared/ui/primitives';

const PROGRESS = (it: VerificationItem) => (it.total_pages ? Math.round((it.verified_pages / it.total_pages) * 100) : 0);

export const QueuePage: React.FC = () => {
  const navigate = useNavigate();
  const { user, hasPermission } = useAuthStore();
  const isAdmin = hasPermission('verification.assign');

  const tabs: { key: QueueScope; label: string }[] = [
    { key: 'available', label: 'Available' },
    { key: 'mine', label: 'My Work' },
    { key: 'submitted', label: 'Submitted' },
    ...(isAdmin ? [{ key: 'all' as QueueScope, label: 'All Files' }] : []),
  ];

  const [scope, setScope] = useState<QueueScope>('available');
  const [items, setItems] = useState<VerificationItem[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const data = await verificationApi.queue(scope);
      setItems(data ?? []);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, [scope]);

  useEffect(() => {
    setLoading(true);
    load();
    const t = setInterval(load, 8000);
    return () => clearInterval(t);
  }, [load]);

  useEffect(() => {
    if (isAdmin) usersApi.list().then(setUsers).catch(() => setUsers([]));
  }, [isAdmin]);

  const claim = async (id: string) => {
    setBusy(id);
    try {
      await verificationApi.claim(id);
      navigate(`/documents/${id}`);
    } catch (e: any) {
      alert(e.message || 'Could not claim this file — it may have just been taken.');
      load();
    } finally {
      setBusy(null);
    }
  };

  const release = async (id: string, force: boolean) => {
    setBusy(id);
    try {
      if (force) await verificationApi.forceRelease(id);
      else await verificationApi.release(id);
      load();
    } catch (e: any) {
      alert(e.message);
    } finally {
      setBusy(null);
    }
  };

  const assign = async (id: string, assignee: string) => {
    try {
      await verificationApi.assign(id, assignee || null);
      load();
    } catch (e: any) {
      alert(e.message);
    }
  };

  return (
    <>
      <PageHeader
        title="Verification Queue"
        subtitle="Pick the next available file — it locks to you the moment you open it"
        actions={<Button variant="ghost" onClick={load}><RefreshCw size={15} /> Refresh</Button>}
      />
      <div className="p-6">
        <div className="mb-4 flex gap-1 rounded-lg border border-slate-800 bg-slate-900 p-1">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setScope(t.key)}
              className={`rounded-md px-4 py-1.5 text-sm font-medium transition-colors ${
                scope === t.key ? 'bg-blue-600 text-white' : 'text-slate-400 hover:text-white'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>

        <Card>
          {loading ? (
            <Spinner label="Loading queue…" />
          ) : items.length === 0 ? (
            <EmptyState
              title={
                scope === 'available'
                  ? 'No files available'
                  : scope === 'mine'
                    ? 'You have no files in progress'
                    : scope === 'submitted'
                      ? 'Nothing submitted yet'
                      : 'No files'
              }
              hint={scope === 'available' ? 'New files appear here once OCR finishes.' : undefined}
            />
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b border-slate-800 text-xs uppercase tracking-wider text-slate-400">
                <tr>
                  <th className="px-4 py-3">File</th>
                  <th className="px-4 py-3">Pages</th>
                  <th className="px-4 py-3">Progress</th>
                  <th className="px-4 py-3">State</th>
                  <th className="px-4 py-3 text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => {
                  const mineLock = it.locked_by && it.locked_by === user?.id;
                  return (
                    <tr key={it.document_id} className="border-b border-slate-800/60 hover:bg-slate-900/40">
                      <td className="px-4 py-3">
                        <p className="max-w-[260px] truncate font-medium text-slate-200" title={it.name}>{it.name}</p>
                        <p className="mt-0.5 text-[10px] text-slate-500">Created {timeAgo(it.created_at)}</p>
                      </td>
                      <td className="px-4 py-3 text-slate-400">{it.total_pages}</td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-24 overflow-hidden rounded-full bg-slate-800">
                            <div className="h-full bg-emerald-500" style={{ width: `${PROGRESS(it)}%` }} />
                          </div>
                          <span className="text-xs text-slate-400">{it.verified_pages}/{it.total_pages}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        {it.verification_status === 'submitted' ? (
                          <Badge color="green">Submitted {it.submitted_by_name ? `· ${it.submitted_by_name}` : ''}</Badge>
                        ) : it.locked_by ? (
                          <Badge color="amber"><Lock size={10} className="mr-1 inline" />{mineLock ? 'You' : it.locked_by_name}{it.current_page ? ` · p.${it.current_page}` : ''}</Badge>
                        ) : it.assigned_to ? (
                          <Badge color="purple">Assigned · {it.assigned_to_name}</Badge>
                        ) : (
                          <Badge color="blue">Available</Badge>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-2">
                          {scope === 'available' && (
                            <Button onClick={() => claim(it.document_id)} disabled={busy === it.document_id}>
                              <Play size={13} className="fill-current" /> Claim & open
                            </Button>
                          )}
                          {scope === 'mine' && (
                            <>
                              <Button onClick={() => navigate(`/documents/${it.document_id}`)}><Play size={13} className="fill-current" /> Continue</Button>
                              <Button variant="ghost" onClick={() => release(it.document_id, false)} disabled={busy === it.document_id}><Unlock size={14} /> Release</Button>
                            </>
                          )}
                          {scope === 'submitted' && (
                            <Button variant="ghost" onClick={() => navigate(`/documents/${it.document_id}`)}>View</Button>
                          )}
                          {scope === 'all' && isAdmin && (
                            <>
                              <Select
                                className="w-40"
                                value={it.assigned_to ?? ''}
                                onChange={(e) => assign(it.document_id, e.target.value)}
                                title="Assign to verifier"
                              >
                                <option value="">Unassigned</option>
                                {users.map((u) => (
                                  <option key={u.id} value={u.id}>{`${u.first_name} ${u.last_name}`.trim() || u.email}</option>
                                ))}
                              </Select>
                              {it.locked_by && (
                                <Button variant="ghost" onClick={() => release(it.document_id, true)} disabled={busy === it.document_id} title="Force-release lock">
                                  <UserCog size={14} /> Force
                                </Button>
                              )}
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Card>
      </div>
    </>
  );
};
