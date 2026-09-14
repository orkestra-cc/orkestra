import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import MarkdownField from './MarkdownField';

const setup = (value: string) => {
  const onChange = vi.fn();
  render(<MarkdownField id="md" value={value} onChange={onChange} />);
  return { onChange };
};

describe('MarkdownField — scrittura', () => {
  it('rende una textarea con il valore e propaga le modifiche', async () => {
    const { onChange } = setup('ciao');
    const ta = screen.getByRole('textbox');
    expect(ta).toHaveValue('ciao');
    expect(ta).toHaveAttribute('id', 'md');
    await userEvent.type(ta, '!');
    expect(onChange).toHaveBeenLastCalledWith('ciao!');
  });

  it('il bottone Bold avvolge la selezione della textarea', async () => {
    const { onChange } = setup('di ciao');
    const ta = screen.getByRole('textbox') as HTMLTextAreaElement;
    ta.focus();
    ta.setSelectionRange(3, 7);
    await userEvent.click(screen.getByRole('button', { name: 'Bold' }));
    expect(onChange).toHaveBeenCalledWith('di **ciao**');
  });
});

describe('MarkdownField — anteprima', () => {
  it('la pill Preview rende il markdown e nasconde la textarea', async () => {
    setup('**forte** e _corsivo_');
    await userEvent.click(screen.getByRole('tab', { name: 'Preview' }));
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.getByText('forte').tagName).toBe('STRONG');
    expect(screen.getByText('corsivo').tagName).toBe('EM');
  });

  it("l'HTML grezzo resta testo, non viene interpretato", async () => {
    setup('<script>window.pwned = 1</script> testo');
    await userEvent.click(screen.getByRole('tab', { name: 'Preview' }));
    expect(document.querySelector('script')).toBeNull();
    expect(screen.getByText(/window\.pwned/)).toBeInTheDocument();
  });

  it('un a-capo singolo diventa un <br>, come si aspetta chi non conosce Markdown', async () => {
    setup('prima riga\nseconda riga');
    await userEvent.click(screen.getByRole('tab', { name: 'Preview' }));
    expect(
      document.querySelector('[data-testid="md-preview"] br')
    ).not.toBeNull();
  });

  it('con valore vuoto mostra un placeholder', async () => {
    setup('');
    await userEvent.click(screen.getByRole('tab', { name: 'Preview' }));
    expect(screen.getByText('Nothing to preview yet.')).toBeInTheDocument();
  });

  it('la pill Write riporta alla textarea', async () => {
    setup('x');
    await userEvent.click(screen.getByRole('tab', { name: 'Preview' }));
    await userEvent.click(screen.getByRole('tab', { name: 'Write' }));
    expect(screen.getByRole('textbox')).toHaveValue('x');
  });
});
