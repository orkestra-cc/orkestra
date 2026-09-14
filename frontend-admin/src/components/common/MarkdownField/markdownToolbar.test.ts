import { describe, it, expect } from 'vitest';
import { applyMarkdownAction } from './markdownToolbar';

describe('applyMarkdownAction — wrap della selezione', () => {
  it('bold avvolge la selezione con ** e la mantiene selezionata', () => {
    const r = applyMarkdownAction('di ciao a tutti', 'bold', {
      start: 3,
      end: 7
    });
    expect(r.text).toBe('di **ciao** a tutti');
    expect(r.text.slice(r.start, r.end)).toBe('ciao');
  });

  it('bold senza selezione inserisce i marker e lascia il cursore in mezzo', () => {
    const r = applyMarkdownAction('di ', 'bold', { start: 3, end: 3 });
    expect(r.text).toBe('di ****');
    expect(r.start).toBe(5);
    expect(r.end).toBe(5);
  });

  it('italic avvolge con _', () => {
    const r = applyMarkdownAction('ciao', 'italic', { start: 0, end: 4 });
    expect(r.text).toBe('_ciao_');
    expect(r.text.slice(r.start, r.end)).toBe('ciao');
  });

  it('link mette la selezione come testo e seleziona il placeholder url', () => {
    const r = applyMarkdownAction('vedi sito', 'link', { start: 5, end: 9 });
    expect(r.text).toBe('vedi [sito](url)');
    expect(r.text.slice(r.start, r.end)).toBe('url');
  });

  it('link senza selezione lascia il testo vuoto e seleziona url', () => {
    const r = applyMarkdownAction('', 'link', { start: 0, end: 0 });
    expect(r.text).toBe('[](url)');
    expect(r.text.slice(r.start, r.end)).toBe('url');
  });
});

describe('applyMarkdownAction — prefisso di riga', () => {
  it('heading prefissa la riga del cursore con ## e sposta il cursore', () => {
    const r = applyMarkdownAction('prima\nseconda', 'heading', {
      start: 8,
      end: 8
    });
    expect(r.text).toBe('prima\n## seconda');
    expect(r.start).toBe(11);
    expect(r.end).toBe(11);
  });

  it('list prefissa ogni riga selezionata con "- "', () => {
    const r = applyMarkdownAction('uno\ndue\ntre', 'list', {
      start: 0,
      end: 7
    });
    expect(r.text).toBe('- uno\n- due\ntre');
    expect(r.text.slice(r.start, r.end)).toBe('- uno\n- due');
  });

  it('quote prefissa ogni riga selezionata con "> "', () => {
    const r = applyMarkdownAction('a\nb', 'quote', { start: 0, end: 3 });
    expect(r.text).toBe('> a\n> b');
  });

  it('un prefisso di riga già presente viene rimosso (toggle)', () => {
    const r = applyMarkdownAction('- uno', 'list', { start: 3, end: 3 });
    expect(r.text).toBe('uno');
    expect(r.start).toBe(1);
  });
});
