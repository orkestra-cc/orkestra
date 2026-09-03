import { Route, Routes } from "react-router";
import { Layout } from "@/components/Layout";
import { RequireAuth } from "@/auth/RequireAuth";
import { HomePage } from "@/pages/HomePage";
import { SignupPage } from "@/pages/SignupPage";
import { VerifyEmailPage } from "@/pages/VerifyEmailPage";
import { LoginPage } from "@/pages/LoginPage";
import { OAuthCallbackPage } from "@/pages/OAuthCallbackPage";
import { ForgotPasswordPage } from "@/pages/ForgotPasswordPage";
import { ResetPasswordPage } from "@/pages/ResetPasswordPage";
import { AcceptInvitePage } from "@/pages/AcceptInvitePage";
import { AccountPage } from "@/pages/AccountPage";
import { AccountProfilePage } from "@/pages/AccountProfilePage";
import { AccountSecurityPage } from "@/pages/AccountSecurityPage";
import { AccountDsrPage } from "@/pages/AccountDsrPage";
import { BillingProfilePage } from "@/pages/BillingProfilePage";
import { MfaEnrolPage } from "@/pages/MfaEnrolPage";
import { NotFoundPage } from "@/pages/NotFoundPage";

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<HomePage />} />
        <Route path="/signup" element={<SignupPage />} />
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/auth/callback" element={<OAuthCallbackPage />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/reset-password" element={<ResetPasswordPage />} />
        <Route path="/accept-invite" element={<AcceptInvitePage />} />
        <Route
          path="/account"
          element={
            <RequireAuth>
              <AccountPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/profile"
          element={
            <RequireAuth>
              <AccountProfilePage />
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
          path="/account/dsr"
          element={
            <RequireAuth>
              <AccountDsrPage />
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
        <Route
          path="/account/billing"
          element={
            <RequireAuth>
              <BillingProfilePage />
            </RequireAuth>
          }
        />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
