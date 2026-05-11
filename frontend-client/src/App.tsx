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
import { AcceptInvitePage } from '@/pages/AcceptInvitePage';
import { AccountPage } from '@/pages/AccountPage';
import { AccountSecurityPage } from '@/pages/AccountSecurityPage';
import { BillingProfilePage } from '@/pages/BillingProfilePage';
import { MfaEnrolPage } from '@/pages/MfaEnrolPage';
import { SubscribePage } from '@/pages/SubscribePage';
import { SubscribeReturnPage } from '@/pages/SubscribeReturnPage';
import { SubscriptionsPage } from '@/pages/SubscriptionsPage';
import { SubscriptionDetailPage } from '@/pages/SubscriptionDetailPage';
import { TransactionsPage } from '@/pages/TransactionsPage';
import { PaymentMethodsPage } from '@/pages/PaymentMethodsPage';
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
        <Route
          path="/account/billing"
          element={
            <RequireAuth>
              <BillingProfilePage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/subscriptions"
          element={
            <RequireAuth>
              <SubscriptionsPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/subscriptions/:id"
          element={
            <RequireAuth>
              <SubscriptionDetailPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/transactions"
          element={
            <RequireAuth>
              <TransactionsPage />
            </RequireAuth>
          }
        />
        <Route
          path="/account/payment-methods"
          element={
            <RequireAuth>
              <PaymentMethodsPage />
            </RequireAuth>
          }
        />
        <Route
          path="/subscribe"
          element={
            <RequireAuth>
              <SubscribePage />
            </RequireAuth>
          }
        />
        <Route
          path="/subscribe/return"
          element={
            <RequireAuth>
              <SubscribeReturnPage />
            </RequireAuth>
          }
        />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
