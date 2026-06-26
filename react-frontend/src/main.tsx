import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { LoginPage } from '@/pages/login/ui/page';
import { DashboardPage } from '@/pages/dashboard/ui/page';
import { VerificationPage } from '@/pages/verification/ui/page';
import { AnalyticsPage } from '@/pages/analytics/ui/page';
import { QueuePage } from '@/pages/queue/ui/page';
import { UsersPage } from '@/pages/users/ui/page';
import { RolesPage } from '@/pages/roles/ui/page';
import { SettingsPage } from '@/pages/settings/ui/page';
import { AppLayout } from '@/shared/ui/app-layout';
import { ProtectedRoute, RequirePermission, Landing } from '@/shared/ui/guards';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <Routes>
      {/* Public */}
      <Route path="/login" element={<LoginPage />} />

      {/* Full-screen verification editor (locks the file to the verifier) */}
      <Route
        path="/documents/:id"
        element={
          <ProtectedRoute>
            <RequirePermission anyOf={['documents.view', 'verification.perform']}>
              <VerificationPage />
            </RequirePermission>
          </ProtectedRoute>
        }
      />

      {/* Everything else lives inside the app shell */}
      <Route
        element={
          <ProtectedRoute>
            <AppLayout />
          </ProtectedRoute>
        }
      >
        <Route index element={<Landing />} />
        <Route path="/dashboard" element={<RequirePermission anyOf={['analytics.view']}><AnalyticsPage /></RequirePermission>} />
        <Route path="/queue" element={<RequirePermission anyOf={['verification.perform']}><QueuePage /></RequirePermission>} />
        <Route path="/documents" element={<RequirePermission anyOf={['documents.view']}><DashboardPage /></RequirePermission>} />
        <Route path="/users" element={<RequirePermission anyOf={['users.view']}><UsersPage /></RequirePermission>} />
        <Route path="/roles" element={<RequirePermission anyOf={['roles.view']}><RolesPage /></RequirePermission>} />
        <Route path="/settings" element={<RequirePermission anyOf={['settings.manage']}><SettingsPage /></RequirePermission>} />
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  </BrowserRouter>
);
