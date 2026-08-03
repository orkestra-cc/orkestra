import { describe, it, expect, beforeAll } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import i18n from '../../../i18n';
import type { ConfigField } from 'store/api/moduleApi';
import ModuleConfigFields from './ModuleConfigFields';
import { buildFieldNames, useModuleConfigForm } from './useModuleConfigForm';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

// ModuleConfigFields no longer owns its values — it registers against a
// react-hook-form instance (Task 1's useModuleConfigForm). This harness is
// the minimal stand-in for what ModuleConfigSection/ModuleConfigPanel wire
// up in production: seed the form from `values`, hand down control+register.
const Harness: React.FC<{
  schema: ConfigField[];
  values: Record<string, string>;
  includeKeys?: string[];
  secretStatus?: Record<string, boolean>;
}> = ({ schema, values, includeKeys, secretStatus }) => {
  const { form, fieldNames } = useModuleConfigForm(schema, values);
  return (
    <ModuleConfigFields
      schema={schema}
      moduleName="demo"
      control={form.control}
      register={form.register}
      fieldNames={fieldNames}
      includeKeys={includeKeys}
      secretStatus={secretStatus}
    />
  );
};

// A sentinel guaranteed to differ from every seeded value in this file (none
// of them contain it), used to force a genuine DOM delta — see the comment
// in `render` below.
const PROBE_SUFFIX = '__probe__';

const render = (schema: ConfigField[], values: Record<string, string>) => {
  const utils = renderWithProviders(
    <Harness schema={schema} values={values} />
  );
  // RHF's mode:'onChange' resolver only runs on interaction, not on mount —
  // so a value seeded straight into defaultValues never gets validated
  // until something changes it. Dispatching a *same-value* change event is
  // not enough to force that: React's input value tracker only fires
  // onChange when the DOM value actually differs from what it last saw, and
  // the DOM already reads `values[key]` (set by RHF from defaultValues on
  // mount), so a same-value fireEvent is silently swallowed. Detour through
  // a different value first so the second change is a genuine delta and
  // actually reaches the resolver, mirroring what a real edit does.
  //
  // The DOM `name` is the register name, not the schema key — they differ the
  // moment a key contains a "." (see `buildFieldNames`), so this probe has to
  // resolve through the same mapping the component registers with.
  const fieldNames = buildFieldNames(schema);
  for (const key of Object.keys(values)) {
    const el = document.querySelector(`[name="${fieldNames.get(key) ?? key}"]`);
    if (!el) continue;
    fireEvent.change(el, {
      target: { value: `${values[key]}${PROBE_SUFFIX}` }
    });
    fireEvent.change(el, { target: { value: values[key] } });
  }
  return utils;
};

describe('duration validation', () => {
  const schema = [field({ key: 'ttl', label: 'TTL', type: 'duration' })];

  it.each(['30s', '15m', '1h', '1h30m', '500ms', '1.5h', '0'])(
    'accepts %s — the backend does',
    async value => {
      render(schema, { ttl: value });
      await waitFor(() =>
        expect(screen.getByLabelText('TTL')).not.toHaveClass('is-invalid')
      );
    }
  );

  it.each(['30 s', 'abc', '15x', '1h30', 'ms'])('rejects %s', async value => {
    render(schema, { ttl: value });
    await waitFor(() =>
      expect(screen.getByLabelText('TTL')).toHaveClass('is-invalid')
    );
  });

  it('treats an empty value as unset, not invalid', async () => {
    render(schema, { ttl: '' });
    await waitFor(() =>
      expect(screen.getByLabelText('TTL')).not.toHaveClass('is-invalid')
    );
  });
});

describe('min / max validation', () => {
  const schema = [
    field({ key: 'len', label: 'Length', type: 'int', min: 8, max: 128 })
  ];

  it('flags a value below min and names the bound', async () => {
    render(schema, { len: '6' });
    expect(await screen.findByText('Minimum is 8')).toBeInTheDocument();
  });

  it('flags a value above max', async () => {
    render(schema, { len: '999' });
    expect(await screen.findByText('Maximum is 128')).toBeInTheDocument();
  });

  it('accepts a value inside the range', async () => {
    render(schema, { len: '12' });
    await waitFor(() => {
      expect(screen.queryByText(/Minimum is/)).not.toBeInTheDocument();
      expect(screen.queryByText(/Maximum is/)).not.toBeInTheDocument();
    });
  });
});

describe('pattern validation', () => {
  it('flags a value that does not match', async () => {
    render([field({ key: 'code', label: 'Code', pattern: '^[a-z]+$' })], {
      code: 'ABC'
    });
    expect(
      await screen.findByText('Value does not match the required format')
    ).toBeInTheDocument();
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
