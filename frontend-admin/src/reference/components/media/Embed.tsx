
import PageHeader from 'components/common/PageHeader';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';

const exampleCode = `<Ratio aspectRatio="16x9">
  <iframe
    src="https://www.youtube.com/embed/zpOULjyy-n8?rel=0"
    allowFullScreen={true}
    title="YouTube video"
  />
</Ratio>
`;

const Embed = () => (
  <>
    <PageHeader
      title="Embed"
      description="Create responsive video or slideshow embeds based on the width of the parent by creating an intrinsic ratio that scales on any device."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example" light={false}>
        <p className="mb-0 mt-2">
          Wrap any embed, like an <code> &lt;iframe&gt;</code> in a parent{' '}
          <code>&lt;Ratio&gt;</code> component with <code> aspectRatio </code>{' '}
          prop.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body code={exampleCode} language="jsx" />
    </OrkestraComponentCard>
  </>
);

export default Embed;
