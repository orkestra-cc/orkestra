import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import SecretOnceDisplay from 'components/common/SecretOnceDisplay';

// SecretOnceDisplayExample is the Orkestra reference showcase for the shared
// shown-once secret primitive (SecretOnceDisplay) — the canonical way to
// surface a value the backend will never return again (a freshly issued
// client secret, API key, ...). Modeled on the same primitive-in-common +
// showcase-in-reference pattern as StatCards.tsx.

const basicCode = `
function BasicExample() {
  const [ack, setAck] = useState(false);
  return (
    <SecretOnceDisplay
      label="Client secret"
      secret="sas_demo_0000000000000000000000000000000000"
      ack={ack}
      onAckChange={setAck}
    />
  );
}
`;

const secondaryValueCode = `
function WithSecondaryExample() {
  const [ack, setAck] = useState(false);
  return (
    <SecretOnceDisplay
      label="Client secret"
      secret="sas_demo_0000000000000000000000000000000000"
      secondaryLabel="Client ID"
      secondaryValue="svc-hermes-agent-7f2a1c"
      ack={ack}
      onAckChange={setAck}
    />
  );
}
`;

const ackGateCode = `
function AckGateExample() {
  const [ack, setAck] = useState(false);
  return (
    <>
      <SecretOnceDisplay
        label="Client secret"
        secret="sas_demo_0000000000000000000000000000000000"
        ack={ack}
        onAckChange={setAck}
      />
      <div className="d-flex justify-content-end mt-3">
        {/* The caller owns the ack state and gates its own "Done"/"Close"
            action on it — exactly like MfaEnrollWizard gates its modal
            close on the backup-codes checkbox. */}
        <Button variant="primary" disabled={!ack}>
          Done
        </Button>
      </div>
    </>
  );
}
`;

const SecretOnceDisplayExample = () => {
  return (
    <>
      <PageHeader
        title="Secret Once Display"
        description="The shown-once secret primitive: a warning banner, the value in a monospace block with a copy-to-clipboard affordance, and a controlled acknowledgement checkbox the caller gates its own close/done action on. No download button — a secret saved to disk is a worse leak surface than the clipboard. Modeled on the userSecurity BackupCodesDisplay pattern."
        className="mb-3"
      />

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Basic usage" light={false}>
          <p className="mb-0">
            <code>ack</code>/<code>onAckChange</code> are fully controlled — the
            component never tracks acknowledgement internally.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={basicCode}
          language="jsx"
          scope={{ SecretOnceDisplay }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header
          title="With a secondary value"
          light={false}
        >
          <p className="mb-0">
            Pass <code>secondaryLabel</code>/<code>secondaryValue</code> for a
            companion field (e.g. a client ID) rendered above the secret — also
            copyable on its own.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={secondaryValueCode}
          language="jsx"
          scope={{ SecretOnceDisplay }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard noGuttersBottom>
        <OrkestraComponentCard.Header
          title="Gating a Done/Close action"
          light={false}
        >
          <p className="mb-0">
            The caller reads back its own <code>ack</code> state to disable a
            confirm button until the operator has acknowledged saving the secret
            — the same shape as <code>MfaEnrollWizard</code>'s
            close-blocked-until-ack modal.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={ackGateCode}
          language="jsx"
          scope={{ SecretOnceDisplay }}
        />
      </OrkestraComponentCard>
    </>
  );
};

export default SecretOnceDisplayExample;
