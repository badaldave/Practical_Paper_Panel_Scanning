import React, { useEffect, useMemo, useState } from 'react';
import { RefreshCw, Circle } from 'lucide-react';
import {
  analyticsApi,
  type Overview,
  type PresenceRow,
  type ProductivityRow,
  type TimeseriesPoint,
  type VerificationEvent,
} from '@/shared/api/services';
import { PageHeader, Card, StatCard, Button, Badge, Input, Spinner, EmptyState, timeAgo } from '@/shared/ui/primitives';

function isoDaysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toISOString().slice(0, 10);
}

const EVENT_LABEL: Record<string, { text: string; color: string }> = {
  claim: { text: 'claimed', color: 'blue' },
  release: { text: 'released', color: 'slate' },
  force_release: { text: 'force-released', color: 'red' },
  assign: { text: 'was assigned', color: 'purple' },
  unassign: { text: 'was unassigned', color: 'slate' },
  page_verified: { text: 'verified a page', color: 'green' },
  page_unverified: { text: 'un-verified a page', color: 'amber' },
  submit: { text: 'submitted', color: 'green' },
};

export const AnalyticsPage: React.FC = () => {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [presence, setPresence] = useState<PresenceRow[]>([]);
  const [activity, setActivity] = useState<VerificationEvent[]>([]);
  const [productivity, setProductivity] = useState<ProductivityRow[]>([]);
  const [series, setSeries] = useState<TimeseriesPoint[]>([]);
  const [from, setFrom] = useState(isoDaysAgo(29));
  const [to, setTo] = useState(isoDaysAgo(0));
  const [loading, setLoading] = useState(true);

  const loadLive = async () => {
    const [o, p, a] = await Promise.all([
      analyticsApi.overview(),
      analyticsApi.presence().catch(() => []),
      analyticsApi.activity(60).catch(() => []),
    ]);
    setOverview(o);
    setPresence(p ?? []);
    setActivity(a ?? []);
  };

  const loadRange = async () => {
    const [prod, ts] = await Promise.all([
      analyticsApi.productivity(from, to).catch(() => []),
      analyticsApi.timeseries(from, to).catch(() => []),
    ]);
    setProductivity(prod ?? []);
    setSeries(ts ?? []);
  };

  useEffect(() => {
    Promise.all([loadLive(), loadRange()]).finally(() => setLoading(false));
    const t = setInterval(loadLive, 10000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    loadRange();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, to]);

  if (loading) return <Spinner label="Loading analytics…" />;

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="Live activity, throughput and verification statistics"
        actions={<Button variant="ghost" onClick={loadLive}><RefreshCw size={15} /> Refresh</Button>}
      />
      <div className="space-y-6 p-6">
        {/* Top KPIs */}
        <section className="grid grid-cols-2 gap-4 md:grid-cols-4 xl:grid-cols-7">
          <StatCard label="Pages verified today" value={overview?.pages_verified_today ?? 0} accent="text-emerald-400" />
          <StatCard label="Files submitted today" value={overview?.files_submitted_today ?? 0} accent="text-emerald-400" />
          <StatCard label="Cell edits today" value={overview?.cell_edits_today ?? 0} accent="text-blue-400" />
          <StatCard label="Pages / month" value={overview?.pages_verified_month ?? 0} />
          <StatCard label="Files / month" value={overview?.files_submitted_month ?? 0} />
          <StatCard label="Active now" value={overview?.active_users ?? 0} accent="text-amber-400" sub={`${overview?.total_users ?? 0} users total`} />
          <StatCard label="Pending review" value={overview?.pending_verification ?? 0} accent="text-amber-400" />
        </section>

        <section className="grid grid-cols-2 gap-4 md:grid-cols-5">
          <StatCard label="Total documents" value={overview?.total_documents ?? 0} />
          <StatCard label="In progress" value={overview?.in_progress ?? 0} accent="text-blue-400" />
          <StatCard label="Submitted" value={overview?.submitted ?? 0} accent="text-emerald-400" />
          <StatCard label="Failed" value={overview?.failed_documents ?? 0} accent="text-red-400" />
          <StatCard
            label="Pages verified"
            value={`${overview?.verified_pages ?? 0}/${overview?.total_pages ?? 0}`}
            sub={overview?.total_pages ? `${Math.round((overview.verified_pages / overview.total_pages) * 100)}% complete` : undefined}
          />
        </section>

        {/* Live presence */}
        <Card className="p-5">
          <div className="mb-3 flex items-center gap-2">
            <Circle size={10} className="animate-pulse fill-emerald-500 text-emerald-500" />
            <h3 className="text-sm font-semibold text-white">Live — who’s working now</h3>
          </div>
          {presence.length === 0 ? (
            <EmptyState title="Nobody is verifying right now" />
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b border-slate-800 text-xs uppercase tracking-wider text-slate-400">
                <tr>
                  <th className="py-2">User</th>
                  <th className="py-2">File</th>
                  <th className="py-2">Page</th>
                  <th className="py-2">Progress</th>
                  <th className="py-2">Held for</th>
                  <th className="py-2">Last action</th>
                </tr>
              </thead>
              <tbody>
                {presence.map((p) => (
                  <tr key={p.document_id} className="border-b border-slate-800/60">
                    <td className="py-2 font-medium text-slate-200">{p.user_name || p.email}</td>
                    <td className="py-2 max-w-[220px] truncate text-slate-400" title={p.document_name}>{p.document_name}</td>
                    <td className="py-2 text-slate-300">{p.current_page ? `${p.current_page}/${p.total_pages}` : '—'}</td>
                    <td className="py-2 text-slate-400">{p.verified_pages}/{p.total_pages}</td>
                    <td className="py-2 text-slate-400">{timeAgo(p.locked_at)}</td>
                    <td className="py-2 text-slate-400">{timeAgo(p.last_activity_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>

        {/* Date range + chart */}
        <Card className="p-5">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <h3 className="text-sm font-semibold text-white">Throughput</h3>
            <div className="flex items-center gap-2 text-sm">
              <span className="text-xs text-slate-400">From</span>
              <Input type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} className="w-auto" />
              <span className="text-xs text-slate-400">To</span>
              <Input type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} className="w-auto" />
            </div>
          </div>
          <BarChart data={series} />
          <div className="mt-3 flex gap-4 text-xs text-slate-400">
            <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-sm bg-emerald-500" /> Pages verified</span>
            <span className="flex items-center gap-1"><span className="inline-block h-2 w-2 rounded-sm bg-blue-500" /> Files submitted</span>
          </div>
        </Card>

        {/* Productivity + Activity */}
        <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card className="p-5">
            <h3 className="mb-3 text-sm font-semibold text-white">Productivity by user</h3>
            {productivity.length === 0 ? (
              <EmptyState title="No activity in this range" />
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b border-slate-800 text-xs uppercase tracking-wider text-slate-400">
                  <tr>
                    <th className="py-2">User</th>
                    <th className="py-2 text-right">Pages</th>
                    <th className="py-2 text-right">Files</th>
                    <th className="py-2 text-right">Cells</th>
                  </tr>
                </thead>
                <tbody>
                  {productivity.map((r) => (
                    <tr key={r.user_id} className="border-b border-slate-800/60">
                      <td className="py-2 text-slate-200">{r.user_name || r.email}</td>
                      <td className="py-2 text-right text-emerald-400">{r.pages_verified}</td>
                      <td className="py-2 text-right text-blue-400">{r.files_submitted}</td>
                      <td className="py-2 text-right text-slate-300">{r.cells_edited}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>

          <Card className="p-5">
            <h3 className="mb-3 text-sm font-semibold text-white">Recent activity</h3>
            {activity.length === 0 ? (
              <EmptyState title="No activity yet" />
            ) : (
              <div className="max-h-80 space-y-2 overflow-y-auto">
                {activity.map((e) => {
                  const meta = EVENT_LABEL[e.event_type] ?? { text: e.event_type, color: 'slate' };
                  return (
                    <div key={e.id} className="flex items-center gap-2 text-sm">
                      <Badge color={meta.color}>{meta.text}</Badge>
                      <span className="text-slate-300">{e.user_name || 'Someone'}</span>
                      {e.document_name && <span className="max-w-[160px] truncate text-slate-500" title={e.document_name}>· {e.document_name}</span>}
                      {e.page_number != null && <span className="text-slate-500">· p.{e.page_number}</span>}
                      <span className="ml-auto whitespace-nowrap text-xs text-slate-500">{timeAgo(e.created_at)}</span>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>
        </section>
      </div>
    </>
  );
};

// Lightweight dependency-free grouped bar chart.
function BarChart({ data }: { data: TimeseriesPoint[] }) {
  const max = useMemo(() => Math.max(1, ...data.map((d) => Math.max(d.pages_verified, d.files_submitted))), [data]);
  if (data.length === 0) return <div className="py-10 text-center text-sm text-slate-500">No data for this range.</div>;
  const showLabels = data.length <= 31;
  return (
    <div className="flex h-44 items-end gap-[3px] overflow-x-auto">
      {data.map((d) => (
        <div key={d.day} className="flex flex-1 flex-col items-center justify-end" style={{ minWidth: 8 }} title={`${d.day}\nPages: ${d.pages_verified}\nFiles: ${d.files_submitted}`}>
          <div className="flex h-36 w-full items-end justify-center gap-[2px]">
            <div className="w-1/2 rounded-t bg-emerald-500" style={{ height: `${(d.pages_verified / max) * 100}%` }} />
            <div className="w-1/2 rounded-t bg-blue-500" style={{ height: `${(d.files_submitted / max) * 100}%` }} />
          </div>
          {showLabels && <div className="mt-1 w-full truncate text-center text-[8px] text-slate-600">{d.day.slice(5)}</div>}
        </div>
      ))}
    </div>
  );
}
