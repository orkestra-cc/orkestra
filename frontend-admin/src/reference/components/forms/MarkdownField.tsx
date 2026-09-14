// src/reference/components/forms/MarkdownField.tsx
// Reference showcase for the shared MarkdownField primitive
// (components/common/MarkdownField): a textarea with a Markdown mini-toolbar
// and a Write / Preview toggle. Use it for any long free-text field whose
// consumer renders Markdown (event descriptions, notes, public copy) instead
// of a bare textarea — one toolbar, one preview, one safe-by-default
// renderer (react-markdown without rehype-raw, with remark-breaks so a
// single newline is a line break).

import { useState } from 'react';
import { Form } from 'react-bootstrap';
import { Controller, useForm } from 'react-hook-form';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import MarkdownField from 'components/common/MarkdownField';

const basicCode = `function BasicExample() {
  const [value, setValue] = useState(
    '## Open Day\\n\\nPorte aperte **sabato** dalle 10.\\n\\n- visita al lab\\n- talk con i founder\\n\\n[Iscriviti](https://example.org)'
  );
  return (
    <>
      <Form.Label htmlFor="md-basic">Description</Form.Label>
      <MarkdownField id="md-basic" value={value} onChange={setValue} />
    </>
  );
}`;

const rhfCode = `function HookFormExample() {
  const { control, handleSubmit } = useForm({
    defaultValues: { description: '' }
  });
  const [saved, setSaved] = useState('');
  return (
    <Form onSubmit={handleSubmit(d => setSaved(d.description))}>
      <Form.Label htmlFor="md-rhf">Description</Form.Label>
      <Controller
        name="description"
        control={control}
        render={({ field }) => (
          <MarkdownField
            id="md-rhf"
            rows={4}
            size="sm"
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
          />
        )}
      />
      <Button type="submit" size="sm" className="mt-2">Save</Button>
      {saved && <pre className="mt-2 mb-0 fs-10">{saved}</pre>}
    </Form>
  );
}`;

const scope = { useState, Form, Controller, useForm, MarkdownField };

const MarkdownFieldShowcase = () => (
  <>
    <PageHeader
      title="Markdown Field"
      description="A controlled textarea with a formatting mini-toolbar (bold, italic, heading, link, list, quote) and a Write / Preview toggle. The preview runs through <strong>react-markdown</strong> without <code>rehype-raw</code>, so raw HTML is shown as text and never mounted, and with <strong>remark-breaks</strong>, so a single newline renders as a line break. A public renderer of the same text must apply both rules."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Basic" light={false}>
        <p className="mb-0">
          Purely controlled: pass <code>value</code> and <code>onChange</code>.
          Toolbar buttons wrap the current selection (or insert the markers at
          the caret); line actions prefix every selected line and toggle off on
          a second click.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={basicCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="With react-hook-form" light={false}>
        <p className="mb-0">
          Wire it through <code>Controller</code> — the field is not a native
          input, so <code>register</code> does not apply. <code>rows</code>,{' '}
          <code>size</code>, <code>isInvalid</code> and <code>disabled</code>{' '}
          mirror the underlying <code>Form.Control</code>.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={rhfCode} scope={scope} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default MarkdownFieldShowcase;
