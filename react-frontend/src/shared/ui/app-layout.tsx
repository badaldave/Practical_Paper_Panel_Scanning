import { useEffect } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import {
  LayoutDashboard,
  ClipboardCheck,
  FileText,
  Users,
  Shield,
  Settings,
  LogOut,
  ScanLine,
} from 'lucide-react';
import { useAuthStore } from '@/entities/user/model/store';
import { meApi } from '@/shared/api/services';
import { COMPANY_SHORT, PRODUCT_NAME } from '@/shared/config/brand';

interface NavItem {
  to: string;
  label: string;
  icon: React.ComponentType<{ size?: number }>;
  perms: string[]; // any-of; empty => always visible
}

const NAV: NavItem[] = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard, perms: ['analytics.view'] },
  { to: '/queue', label: 'Verification Queue', icon: ClipboardCheck, perms: ['verification.perform'] },
  { to: '/documents', label: 'Documents', icon: FileText, perms: ['documents.view'] },
  { to: '/users', label: 'Users', icon: Users, perms: ['users.view'] },
  { to: '/roles', label: 'Roles & Permissions', icon: Shield, perms: ['roles.view'] },
  { to: '/settings', label: 'Settings', icon: Settings, perms: ['settings.manage'] },
];

export function AppLayout() {
  const navigate = useNavigate();
  const { user, logout, setUser, hasAnyPermission } = useAuthStore();

  // Refresh identity + live permissions on mount so role changes take effect
  // without forcing a re-login.
  useEffect(() => {
    meApi
      .get()
      .then(setUser)
      .catch(() => {
        /* 401s are handled by the api client (clears token) */
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleLogout = () => {
    logout();
    navigate('/login', { replace: true });
  };

  const visibleNav = NAV.filter((n) => n.perms.length === 0 || hasAnyPermission(n.perms));
  const fullName = user ? `${user.first_name} ${user.last_name}`.trim() || user.email : '';

  return (
    <div className="flex h-screen overflow-hidden bg-slate-950 text-slate-100">
      {/* Sidebar */}
      <aside className="flex w-64 flex-shrink-0 flex-col border-r border-slate-800 bg-slate-900">
        <div className="flex items-center gap-2 border-b border-slate-800 px-5 py-4">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-blue-600">
            <ScanLine size={20} />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-semibold">{COMPANY_SHORT}</div>
            <div className="text-[11px] text-slate-400">{PRODUCT_NAME}</div>
          </div>
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-4">
          {visibleNav.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  `flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
                    isActive
                      ? 'bg-blue-600/20 text-blue-300'
                      : 'text-slate-300 hover:bg-slate-800 hover:text-white'
                  }`
                }
              >
                <Icon size={18} />
                {item.label}
              </NavLink>
            );
          })}
        </nav>

        <div className="border-t border-slate-800 p-3">
          <div className="mb-2 px-2">
            <div className="truncate text-sm font-medium">{fullName}</div>
            <div className="truncate text-xs text-slate-400">{user?.roles?.join(', ') || 'No role'}</div>
          </div>
          <button
            onClick={handleLogout}
            className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-slate-300 transition-colors hover:bg-red-600/20 hover:text-red-300"
          >
            <LogOut size={18} />
            Sign out
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
