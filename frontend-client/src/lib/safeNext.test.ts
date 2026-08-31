import { describe, expect, it } from "vitest";

import { DEFAULT_POST_LOGIN, sanitizeNext } from "@/lib/safeNext";

const origin = window.location.origin;

describe("sanitizeNext", () => {
  it("exports /account as the fallback destination", () => {
    expect(DEFAULT_POST_LOGIN).toBe("/account");
  });

  it.each([
    ["/account", "/account"],
    ["/account/security?tab=oauth#top", "/account/security?tab=oauth#top"],
    ["  /account/billing  ", "/account/billing"],
    ["/loginx", "/loginx"],
    ["/accounts", "/accounts"],
    ["/Account", "/Account"],
    ["/account/%41", "/account/%41"],
    [
      "/account?redirect=https%3A%2F%2Fevil.example",
      "/account?redirect=https%3A%2F%2Fevil.example",
    ],
  ])("accepts the same-origin relative path %j", (raw, expected) => {
    expect(sanitizeNext(raw)).toBe(expected);
  });

  it.each([
    [null],
    [undefined],
    [""],
    ["   "],
    [42],
    [{ pathname: "/account" }],
    ["account"],
    ["https://evil.example/x"],
    [`${origin}/account`],
    ["//evil.example"],
    ["/\\evil.example"],
    ["/%5Cevil.example"],
    ["/%5cevil.example"],
    ["/%2F%2Fevil.example"],
    ["/%2fevil.example"],
    ["/acc\\ount"],
    ["/a\u0000b"],
    ["/a\nb"],
    ["/account%00x"],
    ["/%E0%A4%A"],
    ["/.//evil.example"],
    ["/%2e//evil.example"],
    ["/a/..//evil.example"],
  ])("rejects %j", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  it.each([
    "/auth/callback",
    "/auth/callback?success=true&provider=google",
    "/auth/callback/",
    "/login",
    "/login/",
    "/login?next=%2Faccount",
    "/signup",
    "/forgot-password",
    "/reset-password?token=abc",
    "/verify-email?token=abc",
    "/accept-invite?token=abc",
  ])("rejects the auth route %s (it would loop or strand the user)", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  // react-router matches routes case-insensitively on the DECODED
  // pathname, so every spelling the router would send to an auth page must
  // be judged as that page (plan F14).
  it.each([
    "/LOGIN",
    "/Login/",
    "/Auth/Callback",
    "/%6cogin",
    "/auth/%63allback",
    "/auth//callback",
    "/login%2Fx",
    "/%2e%2e/login",
    "/account/%2e%2e/login",
    "/account/../login",
    "/account/./../auth/callback",
  ])("rejects the disguised auth route %s", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  it("canonicalises dot segments for accepted paths too", () => {
    expect(sanitizeNext("/account/./security")).toBe("/account/security");
    expect(sanitizeNext("/account/billing/../security")).toBe(
      "/account/security",
    );
  });
});
