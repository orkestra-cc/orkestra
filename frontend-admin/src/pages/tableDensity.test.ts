// Source-level guard: a raw <Table> on a production page carries the
// console's table type size.
//
// DESIGN.md («Data tables») puts every table in this console at `fs-10` —
// 13.33px against the 16px the theme gives body text and form controls. The
// split is deliberate: "Default-size controls carry body-size text — forms
// stay at 1rem even where tables are fs-10", so a table that forgets the
// class does not read as slightly off, it reads as the loudest thing on the
// surface, set larger than the data it exists to let the operator compare.
//
// Nothing enforced it. `AdvanceTable` call sites pass `fs-10` through
// `tableProps` and inherit the rule by copy-paste, but a raw React Bootstrap
// `<Table>` — the sanctioned shape for a short table inside a card or a
// modal — starts at 1rem and stays there. The intake resolver's comparison
// table shipped that way and was the biggest text in its own modal; seven
// tables across the CRM pages were in the same state, all of them by
// omission rather than by decision. This guard is what stops the eighth.
//
// Scope is `src/pages/` on purpose. `src/reference/` is the design-reference
// library, where raw Bootstrap at its default size is the thing being shown;
// `components/common/advance-table/AdvanceTable.tsx` is the primitive whose
// className comes from its caller; `components/dashboards/` are template
// widgets. None of those are production console pages.
//
// If a table genuinely needs another size, say so at the call site with the
// token that carries it (`fs-11` for a denser one) rather than leaving the
// default — the point of the guard is that the size is a decision.
import { describe, it, expect } from 'vitest';

// Read as raw text: evaluating these modules would pull the whole app in.
const sources = import.meta.glob('./**/*.tsx', {
  eager: true,
  query: '?raw',
  import: 'default'
}) as Record<string, string>;

// The opening tag of a React Bootstrap <Table …>, attributes included, up to
// the first `>`. Attributes routinely wrap across lines, hence `[\s\S]`, and
// `<TableRow`-style neighbours are excluded by the word boundary.
const TABLE_TAG = /<Table\b[\s\S]*?>/g;

// `fs-10` is the register; `fs-11` is the one step down, for a table that
// deliberately sits denser than the rest.
const DENSITY = /\bfs-1[01]\b/;

const toSrcPath = (globKey: string) =>
  'src/pages/' + globKey.replace(/^\.\//, '');

const isTestFile = (path: string) =>
  /\.(test|spec)\.[jt]sx?$/.test(path) || path.includes('/__tests__/');

describe('table density guard', () => {
  it('gives every raw <Table> on a page the console type size', () => {
    const offenders = Object.entries(sources)
      .map(([key, source]) => [toSrcPath(key), source] as const)
      .filter(([path]) => !isTestFile(path))
      .flatMap(([path, source]) =>
        (source.match(TABLE_TAG) ?? [])
          .filter(tag => !DENSITY.test(tag))
          .map(() => path)
      )
      .sort();

    expect(offenders).toEqual([]);
  });
});
