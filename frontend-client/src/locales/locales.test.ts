import { describe, expect, it } from "vitest";

import en from "./en.json";
import itBundle from "./it.json";

const flatten = (value: unknown, prefix = ""): string[] =>
  Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    child !== null && typeof child === "object"
      ? flatten(child, `${prefix}${key}.`)
      : [`${prefix}${key}`],
  );

describe("locale bundles", () => {
  it("en.json and it.json declare exactly the same keys", () => {
    const enKeys = flatten(en).sort();
    const itKeys = flatten(itBundle).sort();
    expect(itKeys).toEqual(enKeys);
  });

  it("no value is empty", () => {
    const empty = (bundle: unknown, name: string) =>
      flatten(bundle)
        .filter((key) => {
          const value = key
            .split(".")
            .reduce<unknown>(
              (node, part) => (node as Record<string, unknown>)[part],
              bundle,
            );
          return typeof value === "string" && value.trim() === "";
        })
        .map((key) => `${name}:${key}`);
    expect([...empty(en, "en"), ...empty(itBundle, "it")]).toEqual([]);
  });
});
