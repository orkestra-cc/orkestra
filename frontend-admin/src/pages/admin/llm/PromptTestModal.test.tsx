import { beforeEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { LlmModel } from 'store/api/llmApi';
import PromptTestModal from './PromptTestModal';

const model = {
  uuid: 'm-1',
  name: 'Fast',
  provider: 'openai',
  modelId: 'gpt-5.6-terra'
} as LlmModel;

const bodies: unknown[] = [];
const stubPrompt = (reply: () => Response) =>
  server.use(
    http.post(url('/v1/admin/llm/models/m-1/prompt'), async ({ request }) => {
      bodies.push(await request.json());
      return reply();
    })
  );

const renderModal = () =>
  renderWithProviders(<PromptTestModal model={model} onClose={() => {}} />);

const ask = async (prompt: string) => {
  await userEvent.type(screen.getByLabelText(/^prompt$/i), prompt);
  await userEvent.click(screen.getByRole('button', { name: /^run$/i }));
};

beforeEach(() => {
  bodies.length = 0;
});

describe('PromptTestModal', () => {
  it('sends the prompt and shows the answer, the model asked, usage and finish reason', async () => {
    stubPrompt(() =>
      HttpResponse.json({
        text: 'Hello there',
        usage: { inputTokens: 1200, outputTokens: 34 },
        finishReason: 'stop',
        fallback: false
      })
    );
    renderModal();
    await ask('Say hello');
    expect(await screen.findByText('Hello there')).toBeInTheDocument();
    expect(bodies).toEqual([{ prompt: 'Say hello' }]);
    expect(
      screen.getByText(/answered by fast \(gpt-5\.6-terra\)/i)
    ).toBeInTheDocument();
    expect(
      screen.getByText('1,200 input · 34 output tokens')
    ).toBeInTheDocument();
    expect(screen.getByText(/finish reason: stop/i)).toBeInTheDocument();
  });

  it('flags an answer that came from a fallback model', async () => {
    stubPrompt(() =>
      HttpResponse.json({
        text: 'From elsewhere',
        truncated: true,
        usage: { inputTokens: 1, outputTokens: 2 },
        finishReason: 'length',
        fallback: true
      })
    );
    renderModal();
    await ask('Hi');
    expect(
      await screen.findByText(/answered by a fallback model, not by fast/i)
    ).toBeInTheDocument();
    expect(screen.getByText(/cut at 64 kib/i)).toBeInTheDocument();
  });

  it.each([
    ['llm.budget_exceeded', 402, /monthly llm budget has no room/i],
    ['llm.provider_unavailable', 503, /provider is not reachable/i],
    ['llm.credential_rejected', 422, /provider rejected the credential/i],
    ['llm.plan_quota_exhausted', 429, /plan quota is used up/i],
    ['llm.rate_limited', 429, /too many requests/i]
  ])('explains %s', async (code, status, message) => {
    stubPrompt(() =>
      HttpResponse.json({ code, detail: 'english detail' }, { status })
    );
    renderModal();
    await ask('Hi');
    expect(await screen.findByText(message)).toBeInTheDocument();
  });

  it('says nothing when the caller cancelled the call', async () => {
    stubPrompt(() =>
      HttpResponse.json({ code: 'llm.request_canceled' }, { status: 499 })
    );
    renderModal();
    await ask('Hi');
    await waitFor(() => expect(bodies).toHaveLength(1));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /^run$/i })).toBeEnabled()
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('refuses an empty prompt without calling the backend', async () => {
    renderModal();
    await userEvent.click(screen.getByRole('button', { name: /^run$/i }));
    expect(
      await screen.findByText(/this field is required/i)
    ).toBeInTheDocument();
    await waitFor(() => expect(bodies).toHaveLength(0));
  });

  it('refuses a prompt over 4000 characters', async () => {
    renderModal();
    await userEvent.click(screen.getByLabelText(/^prompt$/i));
    await userEvent.paste('x'.repeat(4001));
    await userEvent.click(screen.getByRole('button', { name: /^run$/i }));
    expect(
      await screen.findByText(/at most 4000 characters/i)
    ).toBeInTheDocument();
    expect(bodies).toHaveLength(0);
  });

  it('counts an emoji as one character, as the backend does', async () => {
    stubPrompt(() =>
      HttpResponse.json({
        text: 'ok',
        usage: { inputTokens: 1, outputTokens: 1 },
        finishReason: 'stop',
        fallback: false
      })
    );
    renderModal();
    await userEvent.click(screen.getByLabelText(/^prompt$/i));
    // 2001 emoji = 4002 UTF-16 units but 2001 characters: under the limit.
    await userEvent.paste('\u{1F600}'.repeat(2001));
    await userEvent.click(screen.getByRole('button', { name: /^run$/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(screen.queryByText(/at most 4000 characters/i)).toBeNull();
  });
});
