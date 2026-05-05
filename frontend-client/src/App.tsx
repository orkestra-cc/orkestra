import { Route, Routes } from 'react-router-dom';
import { Layout } from '@/components/Layout';
import { RequireAuth } from '@/auth/RequireAuth';
import { HomePage } from '@/pages/HomePage';
import { CatalogPage } from '@/pages/CatalogPage';
import { CatalogServicePage } from '@/pages/CatalogServicePage';
import { SignupPage } from '@/pages/SignupPage';
import { VerifyEmailPage } from '@/pages/VerifyEmailPage';
import { LoginPage } from '@/pages/LoginPage';
import { ForgotPasswordPage } from '@/pages/ForgotPasswordPage';
import { ResetPasswordPage } from '@/pages/ResetPasswordPage';
import { AccountPage } from '@/pages/AccountPage';
import { AccountSecurityPage } from '@/pages/AccountSecurityPage';
import { MfaEnrolPage } from '@/pages/MfaEnrolPage';
import { NotFoundPage } from '@/pages/NotFoundPage';

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<HomePage />} />
        <Route path="/catalog" element={<CatalogPage />} />
        <Route path="/catalog/:code" element={<CatalogServicePage />} />
        <Route path="/signup" element={<SignupPage />} />
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/reset-password" element={<ResetPasswordPage />} />
        <Route
          path="/account"
          element={
            <RequireAuth>
              <AccountPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/security"
          element={
            <RequireAuth>
              <AccountSecurityPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/security/mfa"
          element={
            <RequireAuth>
              <MfaEnrolPage />
            </RequireAuth>
          }
        />
        {/* /subscribe + return URLs land in Phase 4. */}
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
