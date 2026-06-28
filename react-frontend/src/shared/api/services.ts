import { apiClient } from './client';
import type { User } from '@/entities/user/model/store';

// ---------- Types ----------
export interface Permission {
  id: string;
  code: string;
  description: string;
  category: string;
}

export interface Role {
  id: string;
  tenant_id?: string | null;
  name: string;
  description: string;
  is_system: boolean;
  permissions: string[];
  user_count: number;
  created_at?: string;
}

export interface VerificationItem {
  document_id: string;
  name: string;
  status: string;
  verification_status: 'pending' | 'in_progress' | 'submitted';
  total_pages: number;
  verified_pages: number;
  locked_by?: string | null;
  locked_by_name?: string;
  locked_at?: string | null;
  assigned_to?: string | null;
  assigned_to_name?: string;
  current_page?: number | null;
  last_activity_at?: string | null;
  submitted_by?: string | null;
  submitted_by_name?: string;
  submitted_at?: string | null;
  created_at: string;
}

export interface PageVerification {
  id: string;
  document_id: string;
  page_number: number;
  is_verified: boolean;
  verified_by?: string | null;
  verified_at?: string | null;
}

export interface DocumentState extends VerificationItem {
  pages: PageVerification[];
}

export interface Overview {
  total_documents: number;
  pending_verification: number;
  in_progress: number;
  submitted: number;
  failed_documents: number;
  total_pages: number;
  verified_pages: number;
  files_submitted_today: number;
  pages_verified_today: number;
  files_submitted_month: number;
  pages_verified_month: number;
  active_users: number;
  total_users: number;
  cell_edits_today: number;
}

export interface PresenceRow {
  user_id: string;
  user_name: string;
  email: string;
  document_id: string;
  document_name: string;
  current_page?: number | null;
  total_pages: number;
  verified_pages: number;
  locked_at?: string | null;
  last_activity_at?: string | null;
}

export interface ProductivityRow {
  user_id: string;
  user_name: string;
  email: string;
  pages_verified: number;
  files_submitted: number;
  cells_edited: number;
}

export interface TimeseriesPoint {
  day: string;
  pages_verified: number;
  files_submitted: number;
  cells_edited: number;
}

export interface VerificationEvent {
  id: string;
  document_id?: string | null;
  page_number?: number | null;
  user_id?: string | null;
  event_type: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  user_name?: string;
  document_name?: string;
}

// ---------- Identity ----------
export const meApi = {
  get: () => apiClient<User>('/api/me'),
};

// ---------- Users ----------
export interface SaveUserBody {
  email?: string;
  password?: string;
  first_name?: string;
  last_name?: string;
  status?: string;
  role_ids?: string[];
}

export const usersApi = {
  list: () => apiClient<User[]>('/api/users'),
  get: (id: string) => apiClient<User>(`/api/users/${id}`),
  create: (body: SaveUserBody) => apiClient<User>('/api/users', { method: 'POST', json: body }),
  update: (id: string, body: SaveUserBody) => apiClient<User>(`/api/users/${id}`, { method: 'PUT', json: body }),
  remove: (id: string) => apiClient<{ message: string }>(`/api/users/${id}`, { method: 'DELETE' }),
  resetPassword: (id: string, password: string) =>
    apiClient<{ message: string }>(`/api/users/${id}/reset-password`, { method: 'POST', json: { password } }),
};

// ---------- Roles & permissions ----------
export interface SaveRoleBody {
  name: string;
  description: string;
  permissions: string[];
}

export const rolesApi = {
  list: () => apiClient<Role[]>('/api/roles'),
  get: (id: string) => apiClient<Role>(`/api/roles/${id}`),
  create: (body: SaveRoleBody) => apiClient<Role>('/api/roles', { method: 'POST', json: body }),
  update: (id: string, body: SaveRoleBody) => apiClient<Role>(`/api/roles/${id}`, { method: 'PUT', json: body }),
  remove: (id: string) => apiClient<{ message: string }>(`/api/roles/${id}`, { method: 'DELETE' }),
  permissions: () => apiClient<Permission[]>('/api/permissions'),
};

// ---------- Verification pool ----------
export type QueueScope = 'available' | 'mine' | 'all' | 'submitted';

export const verificationApi = {
  queue: (scope: QueueScope) => apiClient<VerificationItem[]>(`/api/verification/queue?scope=${scope}`),
  state: (id: string) => apiClient<DocumentState>(`/api/verification/${id}/state`),
  claim: (id: string) => apiClient<DocumentState>(`/api/verification/${id}/claim`, { method: 'POST' }),
  release: (id: string) => apiClient<{ message: string }>(`/api/verification/${id}/release`, { method: 'POST' }),
  forceRelease: (id: string) => apiClient<{ message: string }>(`/api/verification/${id}/force-release`, { method: 'POST' }),
  assign: (id: string, assignee_id: string | null) =>
    apiClient<{ message: string }>(`/api/verification/${id}/assign`, { method: 'POST', json: { assignee_id } }),
  presence: (id: string, page: number) =>
    apiClient<{ message: string }>(`/api/verification/${id}/presence`, { method: 'POST', json: { page } }),
  markPage: (id: string, page: number, verified: boolean) =>
    apiClient<{ message: string }>(`/api/verification/${id}/pages/${page}/verify`, { method: 'POST', json: { verified } }),
  submit: (id: string) => apiClient<{ message: string }>(`/api/verification/${id}/submit`, { method: 'POST' }),
};

// ---------- Examiner directory ----------
export interface ExaminerLookup {
  mobile: string;
  name: string;
  ambiguous: boolean;
  source: string; // 'verified' | 'registry' | '' (none)
}

export const examinersApi = {
  // Best-known examiner name for a 10-digit mobile, using the latest human-verified
  // correction first, then the seeded registry. Returns empty name when unknown or
  // ambiguous.
  lookup: (mobile: string) =>
    apiClient<ExaminerLookup>(`/api/examiners/lookup?mobile=${encodeURIComponent(mobile)}`),
};

// ---------- Settings ----------
export interface TenantSettings {
  low_confidence_threshold: number;
  flag_inferred_values: boolean;
  export_include_confidence: boolean;
}

export interface SettingsResponse {
  organization_name: string;
  domain: string;
  settings: TenantSettings;
}

export interface UpdateSettingsBody {
  organization_name?: string;
  settings?: Partial<TenantSettings>;
}

export const settingsApi = {
  get: () => apiClient<SettingsResponse>('/api/settings'),
  update: (body: UpdateSettingsBody) => apiClient<SettingsResponse>('/api/settings', { method: 'PUT', json: body }),
};

// ---------- Analytics ----------
export const analyticsApi = {
  overview: () => apiClient<Overview>('/api/analytics/overview'),
  presence: () => apiClient<PresenceRow[]>('/api/analytics/presence'),
  activity: (limit = 100) => apiClient<VerificationEvent[]>(`/api/analytics/activity?limit=${limit}`),
  productivity: (from: string, to: string) =>
    apiClient<ProductivityRow[]>(`/api/analytics/productivity?from=${from}&to=${to}`),
  timeseries: (from: string, to: string) =>
    apiClient<TimeseriesPoint[]>(`/api/analytics/timeseries?from=${from}&to=${to}`),
};
