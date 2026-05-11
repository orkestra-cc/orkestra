import { loadStripe, type Stripe } from '@stripe/stripe-js';

// Lazy Stripe.js loader — defers the network request until the first
// payment route is exercised (Phase 4). Returning null when the
// publishable key is unset keeps the scaffold runnable without
// pre-provisioning a Stripe account.
let stripePromise: Promise<Stripe | null> | null = null;

export function getStripe(): Promise<Stripe | null> {
  if (!stripePromise) {
    const key = import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY;
    stripePromise = key ? loadStripe(key) : Promise.resolve(null);
  }
  return stripePromise;
}
