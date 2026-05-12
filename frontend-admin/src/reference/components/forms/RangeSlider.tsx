
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import OrkestraReactRange from './OrkestraReactRange';

const defaultExampleCode = `
  function DefaultExample() {
    const [values, setValues] = useState([20]);
    return (
      <OrkestraReactRange
        values={values}
        variant="primary"
        onChange={val => setValues(val)}
      />
    )
  }
`;
const rangeExampleCode = `
  function RangeExample() {
    const [values, setValues] = useState([20, 70]);
    return (
      <OrkestraReactRange
        values={values}
        variant="primary"
        onChange={val => setValues(val)}
      />
    )
  }
`;
const draggableTrackExampleCode = `
  function RangeExample() {
    const [values, setValues] = useState([10, 55]);
    return (
      <OrkestraReactRange
        draggableTrack
        values={values}
        variant="primary"
        onChange={val => setValues(val)}
      />
    )
  }
`;
const marksExampleCode = `
  function MarksExample() {
    const [values, setValues] = useState([20, 80]);
    return (
      <OrkestraReactRange
        marks
        step={10}
        trackHeight=".3rem"
        values={values}
        onChange={val => setValues(val)}
      />
    )
  }
`;

const variantExampleCode = `
  function RangeExample() {
    const [values, setValues] = useState({
      primary: [20, 50],
      secondary: [30, 60],
      danger: [20, 50],
      warning: [30, 70],
      info: [10, 60],
      success: [15, 70],
    });
    return (
      <>
        <OrkestraReactRange
          values={values['primary']}
          variant="primary"
          onChange={val => setValues({...values, primary: val})}
        />
        <OrkestraReactRange
          values={values['secondary']}
          variant="secondary"
          onChange={val => setValues({...values, secondary: val})}
        />
        <OrkestraReactRange
          values={values['success']}
          variant="success"
          onChange={val => setValues({...values, success: val})}
        />
        <OrkestraReactRange
          values={values['danger']}
          variant="danger"
          onChange={val => setValues({...values, danger: val})}
        />
        <OrkestraReactRange
          values={values['warning']}
          variant="warning"
          onChange={val => setValues({...values, warning: val})}
        />
        <OrkestraReactRange
          values={values['info']}
          variant="info"
          onChange={val => setValues({...values, info: val})}
        />
      </>
    )
  }
`;

const RangeSlider = () => {
  return (
    <>
      <PageHeader
        title="React Range"
        description="Orkestra using React-range for advanced input with a slider which allows bring your own styles and markup."
        className="mb-3"
      >
        <Button
          href="https://github.com/tajo/react-range"
          target="_blank"
          variant="link"
          size="sm"
          className="ps-0"
        >
          React-range Documentation
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Default Example" light={false} />
        <OrkestraComponentCard.Body
          code={defaultExampleCode}
          scope={{ OrkestraReactRange }}
          language="jsx"
        />
      </OrkestraComponentCard>
      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Range Slider" light={false} />
        <OrkestraComponentCard.Body
          code={rangeExampleCode}
          scope={{ OrkestraReactRange }}
          language="jsx"
        />
      </OrkestraComponentCard>
      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Draggable Track" light={false} />
        <OrkestraComponentCard.Body
          code={draggableTrackExampleCode}
          scope={{ OrkestraReactRange }}
          language="jsx"
        />
      </OrkestraComponentCard>
      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="With Marks" light={false} />
        <OrkestraComponentCard.Body
          code={marksExampleCode}
          scope={{ OrkestraReactRange }}
          language="jsx"
        />
      </OrkestraComponentCard>
      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Range Variants" light={false} />
        <OrkestraComponentCard.Body
          code={variantExampleCode}
          scope={{ OrkestraReactRange }}
          language="jsx"
        />
      </OrkestraComponentCard>
      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="React Range" light={false} />
        <OrkestraComponentCard.Body
          code={defaultExampleCode}
          language="jsx"
          scope={{ OrkestraReactRange }}
        ></OrkestraComponentCard.Body>
      </OrkestraComponentCard>
    </>
  );
};

export default RangeSlider;
