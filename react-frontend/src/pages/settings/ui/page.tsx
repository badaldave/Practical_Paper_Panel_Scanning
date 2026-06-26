import React, { useEffect, useState } from 'react';
import { Save } from 'lucide-react';
import { settingsApi, type SettingsResponse, type TenantSettings } from '@/shared/api/services';
import { useAuthStore } from '@/entities/user/model/store';
import { PageHeader, Card, Button, Input, Field, Spinner } from '@/shared/ui/primitives';

export const SettingsPage: React.FC = () => {
  const canManage = useAuthStore((s) => s.hasPermission('settings.manage'));
  const [data, setData] = useState<SettingsResponse | null>(null);
  const [orgName, setOrgName] = useState('');
  const [settings, setSettings] = useState<TenantSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    settingsApi
      .get()
      .then((d) => {
        setData(d);
        setOrgName(d.organization_name);
        setSettings(d.settings);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  const patch = <K extends keyof TenantSettings>(key: K, value: TenantSettings[K]) =>
    setSettings((s) => (s ? { ...s, [key]: value } : s));

  const save = async () => {
    if (!settings) return;
    setSaving(true);
    setError(null);
    try {
      const res = await settingsApi.update({ organization_name: orgName, settings });
      setData(res);
      setSettings(res.settings);
      setOrgName(res.organization_name);
      setSavedAt(Date.now());
    } catch (e: any) {
      setError(e.message || 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <Spinner label="Loading settings…" />;

  return (
    <>
      <PageHeader
        title="Settings"
        subtitle="Organization profile and verification preferences"
        actions={
          canManage && (
            <Button onClick={save} disabled={saving}>
              <Save size={15} /> {saving ? 'Saving…' : 'Save changes'}
            </Button>
          )
        }
      />
      <div className="max-w-3xl space-y-6 p-6">
        {error && <div className="rounded-lg bg-red-950/40 px-3 py-2 text-sm text-red-300">{error}</div>}
        {savedAt && <div className="rounded-lg bg-emerald-950/40 px-3 py-2 text-sm text-emerald-300">Settings saved.</div>}
        {!canManage && <div className="rounded-lg bg-slate-800 px-3 py-2 text-xs text-slate-300">You have read-only access to settings.</div>}

        <Card className="space-y-4 p-5">
          <h3 className="text-sm font-semibold text-white">Organization</h3>
          <Field label="Organization name">
            <Input value={orgName} disabled={!canManage} onChange={(e) => setOrgName(e.target.value)} />
          </Field>
          <Field label="Login domain (read-only)">
            <Input value={data?.domain ?? ''} disabled />
          </Field>
        </Card>

        <Card className="space-y-4 p-5">
          <h3 className="text-sm font-semibold text-white">Verification</h3>

          <Field label={`Low-confidence threshold — ${Math.round((settings?.low_confidence_threshold ?? 0.85) * 100)}%`}>
            <input
              type="range"
              min={0.5}
              max={1}
              step={0.01}
              value={settings?.low_confidence_threshold ?? 0.85}
              disabled={!canManage}
              onChange={(e) => patch('low_confidence_threshold', parseFloat(e.target.value))}
              className="w-full accent-blue-500"
            />
            <p className="mt-1 text-xs text-slate-500">
              Cells extracted below this confidence are highlighted red in the verification grid and counted in the review warning.
            </p>
          </Field>

          <ToggleRow
            label="Flag inferred values"
            hint="Highlight cells auto-filled by cross-document consensus so reviewers double-check them."
            checked={settings?.flag_inferred_values ?? true}
            disabled={!canManage}
            onChange={(v) => patch('flag_inferred_values', v)}
          />
          <ToggleRow
            label="Include confidence in exports"
            hint="Add a confidence column to CSV/Excel exports."
            checked={settings?.export_include_confidence ?? true}
            disabled={!canManage}
            onChange={(v) => patch('export_include_confidence', v)}
          />
        </Card>
      </div>
    </>
  );
};

function ToggleRow({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <div className="text-sm text-slate-200">{label}</div>
        <div className="text-xs text-slate-500">{hint}</div>
      </div>
      <button
        type="button"
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`relative mt-1 h-6 w-11 flex-shrink-0 rounded-full transition-colors disabled:opacity-50 ${checked ? 'bg-blue-600' : 'bg-slate-700'}`}
      >
        <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${checked ? 'left-0.5 translate-x-5' : 'left-0.5'}`} />
      </button>
    </div>
  );
}
