// Example EN/IT parity test for an addon's locale bundles (ADR-0007).
//
// Copy to `<your module dir>/locales/parity.test.ts` (e.g.
// src/pages/widgets/locales/parity.test.ts). It reuses the SAME primitives
// the core parity test uses, so your addon validates its own namespace
// without touching the core locale files. Named `.example.ts` here so it
// stays inert inside the scaffold (vitest only runs `*.test.ts`), exactly
// like `routes.example.tsx`.
//
// ----------------------------------------------------------------------
// import { describe, expect, it } from 'vitest';
// import { emptyValues, keyDiff } from 'locales/parityCheck';
// import en from './en.json';
// import it from './it.json';
//
// describe('widgets locale parity', () => {
//   it('en and it carry the same keys', () => {
//     const { onlyInA: onlyInEn, onlyInB: onlyInIt } = keyDiff(en, it);
//     expect({ onlyInEn, onlyInIt }).toStrictEqual({
//       onlyInEn: [],
//       onlyInIt: []
//     });
//   });
//
//   it('no empty values', () => {
//     expect([...emptyValues(en, 'en'), ...emptyValues(it, 'it')]).toEqual([]);
//   });
// });
// ----------------------------------------------------------------------

export {};
