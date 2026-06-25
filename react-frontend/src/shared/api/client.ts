export interface APIRequestOptions extends RequestInit {
  json?: any;
}

export class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'APIError';
  }
}

export async function apiClient<T>(path: string, options: APIRequestOptions = {}): Promise<T> {
  const token = localStorage.getItem('auth_token');
  
  const headers = new Headers(options.headers);
  headers.set('Content-Type', 'application/json');
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  const reqOptions: RequestInit = {
    ...options,
    headers,
  };

  if (options.json) {
    reqOptions.body = JSON.stringify(options.json);
  }

  const response = await fetch(path, reqOptions);

  if (!response.ok) {
    if (response.status === 401) {
      localStorage.removeItem('auth_token');
      localStorage.removeItem('auth_user');
    }
    let errMsg = `Request failed with status ${response.status}`;
    try {
      const errBody = await response.json();
      errMsg = errBody.error || errMsg;
    } catch {
      // Ignore body parsing failures on raw errors
    }
    throw new APIError(response.status, errMsg);
  }

  if (response.status === 204) {
    return {} as T;
  }

  return response.json();
}
