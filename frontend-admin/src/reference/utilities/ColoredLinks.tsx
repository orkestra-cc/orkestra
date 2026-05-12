
import PageHeader from 'components/common/PageHeader';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { Link } from 'react-router';

const exampleCode = `
<>
  {['primary', 'secondary', 'success', 'info', 'warning', 'danger', 'light', 'dark'].map(
    (color) => (
      <Link to="#!" className={'d-block link-' + color} key={color} >
        {color} link
      </Link>
    )
  )}
</>`;
const graysCode = `
<>
  {
    [
      '1100',
      '1000',
      '900',
      '800',
      '700',
      '600',
      '500',
      '400',
      '300',
      '200',
      '100'
    ].map(
      (color) => (
        <Link to="#!" className={'d-block link-' + color} key={color} >
          Link {color}
        </Link>
      )
    )
  }
</>`;

const ColoredLinks = () => (
  <>
    <PageHeader
      title="Colored links"
      description="Colored links with hover states"
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example">
        <p className="mb-0 mt-2">
          You can use the <code>.link-*</code> classes to colorize links. Unlike
          the{' '}
          <a
            href="https://getbootstrap.com/docs/5.0/helpers/colored-links/"
            target="_blank"
            rel="noreferrer"
          >
            <code>.text-*</code> classes
          </a>
          , these classes have a <code>:hover</code> and <code>:focus</code>{' '}
          state.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={exampleCode}
        scope={{ Link }}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Grays" />
      <OrkestraComponentCard.Body
        code={graysCode}
        scope={{ Link }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default ColoredLinks;
