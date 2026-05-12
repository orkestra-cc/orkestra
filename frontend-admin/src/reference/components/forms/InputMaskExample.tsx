
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { IMaskInput } from 'react-imask';

const exampleCode = `
  <>
    <Form.Group className="mb-3" controlId="exampleForm.DateInput">
      <Form.Label>Date</Form.Label>
      <IMaskInput
        mask="00/00/0000"
        className="form-control"
        placeholder="DD/MM/YYYY"
        id="exampleForm.DateInput"
      />
    </Form.Group>
    <Form.Group className="mb-3" controlId="exampleForm.TimeInput">
      <Form.Label>Time</Form.Label>
      <IMaskInput
        mask="00:00:00"
        className="form-control"
        placeholder="HH:MM:SS"
        id="exampleForm.TimeInput"
      />
    </Form.Group>
    <Form.Group className="mb-3" controlId="exampleForm.USPhoneInput">
      <Form.Label>US phone number</Form.Label>
      <IMaskInput
        mask="(000) 000-0000"
        className="form-control"
        placeholder="(XXX) XXX-XXXX"
        id="exampleForm.USPhoneInput"
        definitions={{
          '0': /[0-9]/,
          'X': /[1-9]/ // If you want to enforce first digit 1-9
        }}
      />
    </Form.Group>
    <Form.Group className="mb-3" controlId="exampleForm.USPhoneCCInput">
      <Form.Label>US phone number (country code)</Form.Label>
      <IMaskInput
        mask="+1 (000) 000-0000"
        className="form-control"
        placeholder="+1 (XXX) XXX-XXXX"
        id="exampleForm.USPhoneCCInput"
      />
    </Form.Group>
    <Form.Group className="mb-3" controlId="exampleForm.CreditCardInput">
      <Form.Label>Credit card</Form.Label>
      <IMaskInput
        mask="0000 0000 0000 0000"
        className="form-control"
        placeholder="XXXX XXXX XXXX XXXX"
        id="exampleForm.CreditCardInput"
      />
    </Form.Group>
    <Form.Group className="mb-3" controlId="exampleForm.IPInput">
      <Form.Label>IP Address</Form.Label>
      <IMaskInput
       mask="000.00.00.00"
        className="form-control"
        placeholder="XXX.XX.XX.XX"
        id="exampleForm.IPInput"
      />
    </Form.Group>
  </>
`;

const InputMaskExample = () => (
  <>
    <PageHeader
      title="Input Mask"
      description="Orkestra-React uses <strong>imask</strong> and <strong>react-imask</strong> for masking input components."
      className="mb-3"
    >
      <Button
        href="https://imask.js.org/"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React Text Mask Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Inputmask Examples" light={false} />
      <OrkestraComponentCard.Body
        code={exampleCode}
        scope={{ IMaskInput }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default InputMaskExample;
