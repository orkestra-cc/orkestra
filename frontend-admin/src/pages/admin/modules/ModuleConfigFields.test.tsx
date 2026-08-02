import { describe, it, expect, beforeAll } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import i18n from '../../../i18n';
import type { ConfigField } from 'store/api/moduleApi';
import ModuleConfigFields from './ModuleConfigFields';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

const render = (schema: ConfigField[], values: Record<string, string>) =>
  renderWithProviders(
    <ModuleConfigFields
      schema={schema}
      moduleName="demo"
      configValues={values}
      secretValues={{}}
      onConfigChange={() => {}}
      onSecretChange={() => {}}
    />
  );

describe('duration validation', () => {
  const schema = [field({ key: 'ttl', label: 'TTL', type: 'duration' })];

  it.each(['30s', '15m', '1h', '1h30m', '500ms', '1.5h', '0'])(
    'accepts %s — the backend does',
    value => {
      render(schema, { ttl: value });
      expect(screen.getByLabelText('TTL')).not.toHaveClass('is-invalid');
    }
  );

  it.each(['30 s', 'abc', '15x', '1h30', 'ms'])('rejects %s', value => {
    render(schema, { ttl: value });
    expect(screen.getByLabelText('TTL')).toHaveClass('is-invalid');
  });

  it('treats an empty value as unset, not invalid', () => {
    render(schema, { ttl: '' });
    expect(screen.getByLabelText('TTL')).not.toHaveClass('is-invalid');
  });
});

describe('min / max validation', () => {
  const schema = [
    field({ key: 'len', label: 'Length', type: 'int', min: 8, max: 128 })
  ];

  it('flags a value below min and names the bound', () => {
    render(schema, { len: '6' });
    expect(screen.getByText('Minimum is 8')).toBeInTheDocument();
  });

  it('flags a value above max', () => {
    render(schema, { len: '999' });
    expect(screen.getByText('Maximum is 128')).toBeInTheDocument();
  });

  it('accepts a value inside the range', () => {
    render(schema, { len: '12' });
    expect(screen.queryByText(/Minimum is/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Maximum is/)).not.toBeInTheDocument();
  });
});

describe('pattern validation', () => {
  it('flags a value that does not match', () => {
    render([field({ key: 'code', label: 'Code', pattern: '^[a-z]+$' })], {
      code: 'ABC'
    });
    expect(
      screen.getByText('Value does not match the required format')
    ).toBeInTheDocument();
  });

  it('ignores an uncompilable pattern rather than throwing', () => {
    // The backend validator rejects these, but a bad regex reaching the render
    // path must degrade to "no pattern check", never crash the page.
    expect(() =>
      render([field({ key: 'code', label: 'Code', pattern: '([' })], {
        code: 'anything'
      })
    ).not.toThrow();
  });
});

describe('placeholder', () => {
  it('prefers the declared placeholder over the default', () => {
    render(
      [
        field({
          key: 'host',
          label: 'Host',
          default: 'localhost',
          placeholder: 'smtp.example.com'
        })
      ],
      {}
    );
    expect(screen.getByPlaceholderText('smtp.example.com')).toBeInTheDocument();
  });

  it('honours the declared placeholder on a stringList too', () => {
    // The textarea branch used to skip field.placeholder entirely and jump
    // straight to default → localized hint, so the same schema produced two
    // different behaviours depending on the field type.
    render(
      [
        field({
          key: 'origins',
          label: 'Origins',
          type: 'stringList',
          default: 'a,b',
          placeholder: 'https://example.com, https://other.test'
        })
      ],
      {}
    );
    expect(
      screen.getByPlaceholderText('https://example.com, https://other.test')
    ).toBeInTheDocument();
  });
});

describe('label association', () => {
  // getByLabelText only resolves through htmlFor/id (or a wrapping <label>);
  // a label rendered as a bare sibling announces as "edit text, blank".
  it('associates the label with a secret input', () => {
    render([field({ key: 'apiKey', label: 'API Key', type: 'secret' })], {});
    expect(screen.getByLabelText('API Key')).toHaveAttribute(
      'type',
      'password'
    );
  });

  it('associates the label with a bool switch', () => {
    render([field({ key: 'on', label: 'Enabled', type: 'bool' })], {});
    expect(screen.getByLabelText('Enabled')).toHaveAttribute(
      'type',
      'checkbox'
    );
  });
});

describe('description', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
    // Injected at runtime, not written into src/locales/*.json — no module
    // ships a `moduleConfig.*` key yet and this branch must not add one.
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          demo: { fields: { translatedOnly: { desc: 'Only from i18n' } } }
        }
      },
      true,
      true
    );
  });

  it('renders a description that only i18n supplies', () => {
    // The guards used to test `field.description` (the raw backend literal)
    // while rendering the resolved string. A field with an empty Go
    // Description but a translated `moduleConfig.<mod>.fields.<key>.desc`
    // resolved fine and rendered nothing, with no signal anywhere.
    render(
      [
        field({
          key: 'translatedOnly',
          label: 'Translated only',
          description: '',
          type: 'secret'
        })
      ],
      {}
    );
    expect(screen.getByText('Only from i18n')).toBeInTheDocument();
  });
});
