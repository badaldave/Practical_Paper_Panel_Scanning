import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { LoginPage } from '@/pages/login/ui/page';
import { DashboardPage } from '@/pages/dashboard/ui/page';
import { VerificationPage } from '@/pages/verification/ui/page';
import './index.css';

// Route Guard to protect workspace paths
const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
  const token = localStorage.getItem('auth_token');
  if (!token) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
};

const renderApp = () => {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <BrowserRouter>
      <Routes>
        {/* Public Routes */}
        <Route path="/login" element={<LoginPage />} />

        {/* Protected Dashboard Registry */}
        <Route 
          path="/documents" 
          element={
            <ProtectedRoute>
              <DashboardPage />
            </ProtectedRoute>
          } 
        />

        {/* Protected Split-Screen Verification Grid */}
        <Route 
          path="/documents/:id" 
          element={
            <ProtectedRoute>
              <VerificationPage />
            </ProtectedRoute>
          } 
        />
        
        {/* Catch-all Redirect to Dashboard */}
        <Route path="*" element={<Navigate to="/documents" replace />} />
      </Routes>
    </BrowserRouter>
  );
};

// Mount application instantly
renderApp();
