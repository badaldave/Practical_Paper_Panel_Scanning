export interface APIRequestOptions extends RequestInit {
  json?: any;
}

export class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'APIError';
  }
}

// Clear all auth artifacts and bounce to the login screen. Called only when a
// refresh genuinely fails (no refresh token, or the refresh itself is rejected).
function clearSession() {
  localStorage.removeItem('auth_token');
  localStorage.removeItem('refresh_token');
  localStorage.removeItem('auth_user');
  // Hard redirect so any in-flight React state is discarded.
  if (window.location.pathname !== '/login') {
    window.location.assign('/login');
  }
}

// A single in-flight refresh shared across all concurrent callers. Without this,
// a burst of polls all hitting a 401 at once would each fire their own refresh,
// rotating the refresh token out from under one another and logging the user
// out. The first caller to see a 401 starts the refresh; everyone else awaits
// the same promise.
let refreshInFlight: Promise<string | null> | null = null;

async function refreshAccessToken(): Promise<string | null> {
  const refreshToken = localStorage.getItem('refresh_token');
  if (!refreshToken) return null;

  const res = await fetch('/api/auth/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });

  if (!res.ok) return null;

  const data = await res.json();
  if (!data.token) return null;

  localStorage.setItem('auth_token', data.token);
  if (data.refresh_token) {
    localStorage.setItem('refresh_token', data.refresh_token);
  }
  if (data.user) {
    localStorage.setItem('auth_user', JSON.stringify(data.user));
  }
  return data.token as string;
}

// Coalesce concurrent refresh attempts onto a single network round-trip.
function getRefreshedToken(): Promise<string | null> {
  if (!refreshInFlight) {
    refreshInFlight = refreshAccessToken().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

function buildRequest(path: string, options: APIRequestOptions, token: string | null): { url: string; init: RequestInit } {
  const headers = new Headers(options.headers);
  headers.set('Content-Type', 'application/json');
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  const init: RequestInit = { ...options, headers };
  if (options.json) {
    init.body = JSON.stringify(options.json);
  }
  return { url: path, init };
}

async function toError(response: Response): Promise<APIError> {
  let errMsg = `Request failed with status ${response.status}`;
  try {
    const errBody = await response.json();
    errMsg = errBody.error || errMsg;
  } catch {
    // Ignore body parsing failures on raw errors
  }
  return new APIError(response.status, errMsg);
}

export async function apiClient<T>(path: string, options: APIRequestOptions = {}): Promise<T> {
  const token = localStorage.getItem('auth_token');
  const { url, init } = buildRequest(path, options, token);

  let response = await fetch(url, init);

  // Access token expired/rejected — try a single transparent refresh + retry
  // before giving up. This is what keeps a long-lived progress tab alive past
  // the 24h access-token lifetime without forcing a re-login.
  if (response.status === 401) {
    const newToken = await getRefreshedToken();
    if (newToken) {
      const retry = buildRequest(path, options, newToken);
      response = await fetch(retry.url, retry.init);
    }

    // Still unauthorized after refresh (or refresh unavailable) → real logout.
    if (response.status === 401) {
      clearSession();
      throw await toError(response);
    }
  }

  if (!response.ok) {
    throw await toError(response);
  }

  if (response.status === 204) {
    return {} as T;
  }

  return response.json();
}
