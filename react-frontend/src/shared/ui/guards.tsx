import { Navigate } from 'react-router-dom';
import { useAuthStore } from '@/entities/user/model/store';

export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('auth_token');
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

// Picks the best landing route for the signed-in user based on their permissions.
export function landingPath(perms: string[]): string {
  if (perms.includes('analytics.view')) return '/dashboard';
  if (perms.includes('verification.perform')) return '/queue';
  if (perms.includes('documents.view')) return '/documents';
  if (perms.includes('users.view')) return '/users';
  if (perms.includes('roles.view')) return '/roles';
  return '/documents';
}

export function Landing() {
  const perms = useAuthStore((s) => s.user?.permissions ?? []);
  return <Navigate to={landingPath(perms)} replace />;
}

export function RequirePermission({ anyOf, children }: { anyOf: string[]; children: React.ReactNode }) {
  const perms = useAuthStore((s) => s.user?.permissions ?? []);
  const allowed = anyOf.length === 0 || anyOf.some((c) => perms.includes(c));
  if (!allowed) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-10 text-center">
        <div className="text-lg font-semibold text-white">Access denied</div>
        <p className="max-w-sm text-sm text-slate-400">
          You don’t have permission to view this page. Contact an administrator if you believe this is a mistake.
        </p>
      </div>
    );
  }
  return <>{children}</>;
}
