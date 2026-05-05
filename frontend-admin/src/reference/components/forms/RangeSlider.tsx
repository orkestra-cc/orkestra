
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import FalconReactRange from './FalconReactRange';

const defaultExampleCode = `
  function DefaultExample() {
    const [values, setValues] = useState([20]);
    return (
      <FalconReactRange
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
      <FalconReactRange
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
      <FalconReactRange
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
      <FalconReactRange
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
        <FalconReactRange
          values={values['primary']}
          variant="primary"
          onChange={val => setValues({...values, primary: val})}
        />
        <FalconReactRange
          values={values['secondary']}
          variant="secondary"
          onChange={val => setValues({...values, secondary: val})}
        />
        <FalconReactRange
          values={values['success']}
          variant="success"
          onChange={val => setValues({...values, success: val})}
        />
        <FalconReactRange
          values={values['danger']}
          variant="danger"
          onChange={val => setValues({...values, danger: val})}
        />
        <FalconReactRange
          values={values['warning']}
          variant="warning"
          onChange={val => setValues({...values, warning: val})}
        />
        <FalconReactRange
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
        description="Falcon using React-range for advanced input with a slider which allows bring your own styles and markup."
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

      <FalconComponentCard>
        <FalconComponentCard.Header title="Default Example" light={false} />
        <FalconComponentCard.Body
          code={defaultExampleCode}
          scope={{ FalconReactRange }}
          language="jsx"
        />
      </FalconComponentCard>
      <FalconComponentCard>
        <FalconComponentCard.Header title="Range Slider" light={false} />
        <FalconComponentCard.Body
          code={rangeExampleCode}
          scope={{ FalconReactRange }}
          language="jsx"
        />
      </FalconComponentCard>
      <FalconComponentCard>
        <FalconComponentCard.Header title="Draggable Track" light={false} />
        <FalconComponentCard.Body
          code={draggableTrackExampleCode}
          scope={{ FalconReactRange }}
          language="jsx"
        />
      </FalconComponentCard>
      <FalconComponentCard>
        <FalconComponentCard.Header title="With Marks" light={false} />
        <FalconComponentCard.Body
          code={marksExampleCode}
          scope={{ FalconReactRange }}
          language="jsx"
        />
      </FalconComponentCard>
      <FalconComponentCard>
        <FalconComponentCard.Header title="Range Variants" light={false} />
        <FalconComponentCard.Body
          code={variantExampleCode}
          scope={{ FalconReactRange }}
          language="jsx"
        />
      </FalconComponentCard>
      <FalconComponentCard>
        <FalconComponentCard.Header title="React Range" light={false} />
        <FalconComponentCard.Body
          code={defaultExampleCode}
          language="jsx"
          scope={{ FalconReactRange }}
        ></FalconComponentCard.Body>
      </FalconComponentCard>
    </>
  );
};

export default RangeSlider;
