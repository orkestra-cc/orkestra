import { describe, expect, it } from "vitest";

import { jwtExp } from "@/lib/jwtExp";

// Builds a JWT-shaped string whose payload segment is EXACTLY the given JSON.
// Written to take raw JSON text rather than an object because two of the cases
// below cannot survive JSON.stringify: `{"exp":1e400}` parses to Infinity, and
// JSON.stringify(Infinity) emits `null`, which would collapse the case into a
// different one and prove nothing.
const tokenWithPayloadJSON = (json: string): string => {
  const seg = Buffer.from(json, "utf8").toString("base64url");
  return `h.${seg}.s`;
};

describe("jwtExp", () => {
  it("reads exp and returns it in the Date.now() domain (milliseconds)", () => {
    expect(jwtExp(tokenWithPayloadJSON('{"exp":1700000000}'))).toBe(
      1700000000 * 1000,
    );
  });

  it("returns null for a token with no exp", () => {
    expect(jwtExp(tokenWithPayloadJSON('{"sub":"u-1"}'))).toBeNull();
  });

  it.each([
    ["null", null],
    ["empty string", ""],
    ["undefined", undefined],
    ["two segments", "h.e"],
    ["four segments", "h.e.s.x"],
    ["non-base64 payload segment", "h.!!!!.s"],
    [
      "payload that is valid base64 but not JSON",
      `h.${Buffer.from("not json").toString("base64url")}.s`,
    ],
    [
      "payload that is JSON but not an object",
      `h.${Buffer.from("42").toString("base64url")}.s`,
    ],
    // The one JSON value `typeof payload !== "object"` does NOT catch:
    // typeof null IS "object". Only the explicit `payload === null` check
    // stops it, and without that check the very next line — reading `.exp`
    // off it — throws a TypeError out of a function documented never to.
    ["payload that is the literal null", tokenWithPayloadJSON("null")],
  ])("returns null for %s", (_label, input) => {
    expect(jwtExp(input as string | null | undefined)).toBeNull();
  });

  // The typeof family: everything here has a non-number exp and must read as
  // "unknown", which §4.3 branch 2 then treats as LIVE — an unknown expiry
  // cannot prove the handler never ran.
  it.each([
    ["a string", '{"exp":"1700000000"}'],
    ["null", '{"exp":null}'],
    ["a boolean", '{"exp":true}'],
    ["an object", '{"exp":{"n":1}}'],
    ["an array", '{"exp":[1700000000]}'],
  ])("returns null when exp is %s", (_label, json) => {
    expect(jwtExp(tokenWithPayloadJSON(json))).toBeNull();
  });

  // The two that slip past `typeof exp === "number"`. They are valid JSON,
  // they survive JSON.parse, and their typeof IS "number". An infinite exp
  // would read as a token that never expires, so branch 2 would pass through
  // EVERY 401 forever and the recovery would be silently disabled; -1e400 is
  // the mirror, refreshing on every 401.
  it.each([
    ["1e400 (Infinity)", '{"exp":1e400}'],
    ["-1e400 (-Infinity)", '{"exp":-1e400}'],
  ])("returns null for a non-finite exp: %s", (_label, json) => {
    // The fixture asserts itself: if this ever stops being a non-finite
    // number, the case is testing something else.
    const parsed = JSON.parse(json) as { exp: number };
    expect(typeof parsed.exp).toBe("number");
    expect(Number.isFinite(parsed.exp)).toBe(false);
    expect(jwtExp(tokenWithPayloadJSON(json))).toBeNull();
  });

  // base64url: `-` and `_` are NOT in base64's alphabet and atob throws
  // InvalidCharacterError on them. Most ASCII JSON never produces those
  // characters — a sweep of 2000 candidate payloads found none — so the
  // fixture is CHOSEN and then asserts its own shape, or the case is vacuous.
  it.each([
    ["a payload containing `_`", '{"exp":1700000000,"s":"?"}'],
    ["a payload containing `-`", '{"exp":1700000000,"s":"~~"}'],
  ])("decodes the base64url alphabet: %s", (_label, json) => {
    const seg = Buffer.from(json, "utf8").toString("base64url");
    expect(seg).toMatch(/[-_]/); // the guard that makes this case real
    expect(jwtExp(`h.${seg}.s`)).toBe(1700000000 * 1000);
  });

  // Padding is TOLERATED by atob in both Node and happy-dom (probed at lengths
  // ≡ 1, 2 and 3 mod 4), so these pass even against a naive implementation.
  // They pin the behaviour against a stricter runtime — the WHATWG
  // forgiving-base64 algorithm does specify failure at length ≡ 1 mod 4 — and
  // a green run here is NOT evidence that the alphabet is handled. The two
  // cases above are.
  it.each([
    ['{"exp":1700000000}'],
    ['{"exp":1700000000,"s":"a"}'],
    ['{"exp":1700000000,"s":"ab"}'],
  ])("tolerates a stripped-padding segment: %s", (json) => {
    const seg = Buffer.from(json, "utf8").toString("base64").replace(/=+$/, "");
    expect(jwtExp(`h.${seg}.s`)).toBe(1700000000 * 1000);
  });
});
