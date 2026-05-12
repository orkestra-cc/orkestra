
import PageHeader from 'components/common/PageHeader';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';

const exampleCode = `
<>
  <div className="float-start">Float left on all viewport sizes</div>
  <br />
  <div className="float-end">Float right on all viewport sizes</div>
  <br />
  <div className="float-none">Don't float on all viewport sizes</div>
</>
`;

const Float = () => (
  <>
    <PageHeader
      title="Float"
      description="Indicate the current page’s location within a navigational hierarchy that automatically adds separators via CSS."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Overview" light={false}>
        <p className="mb-0 mt-2">
          These utility classes float an element to the left or right, or
          disable floating, based on the current viewport size using the
          <a href="https://developer.mozilla.org/en-US/docs/Web/CSS/float">
            CSS <code>float</code> property
          </a>
          . <code>!important</code> is included to avoid specificity issues.
          These use the same viewport breakpoints as our grid system. Please be
          aware float utilities have no effect on flex items.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Float;
