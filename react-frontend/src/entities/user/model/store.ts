import { create } from 'zustand';

export interface User {
  id: string;
  tenant_id: string;
  email: string;
  first_name: string;
  last_name: string;
  status: string;
  roles: string[];
  permissions: string[];
}

interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  setAuth: (user: User, token: string) => void;
  setUser: (user: User) => void;
  hasPermission: (code: string) => boolean;
  hasAnyPermission: (codes: string[]) => boolean;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set, get) => {
  // Load initial token if it exists in local storage
  const savedToken = localStorage.getItem('auth_token');
  const savedUserStr = localStorage.getItem('auth_user');
  let savedUser = null;
  if (savedUserStr) {
    try {
      savedUser = JSON.parse(savedUserStr);
    } catch {
      localStorage.removeItem('auth_user');
    }
  }

  return {
    user: savedUser,
    token: savedToken,
    isAuthenticated: !!savedToken,
    setAuth: (user, token) => {
      localStorage.setItem('auth_token', token);
      localStorage.setItem('auth_user', JSON.stringify(user));
      set({ user, token, isAuthenticated: true });
    },
    setUser: (user) => {
      localStorage.setItem('auth_user', JSON.stringify(user));
      set({ user });
    },
    hasPermission: (code) => {
      const u = get().user;
      return !!u?.permissions?.includes(code);
    },
    hasAnyPermission: (codes) => {
      const perms = get().user?.permissions ?? [];
      return codes.some((c) => perms.includes(c));
    },
    logout: () => {
      localStorage.removeItem('auth_token');
      localStorage.removeItem('refresh_token');
      localStorage.removeItem('auth_user');
      set({ user: null, token: null, isAuthenticated: false });
    },
  };
});
