
import PageHeader from 'components/common/PageHeader';
import FalconComponentCard from 'components/common/FalconComponentCard';

const textColorsCode = `<>
  <h5 class="text-primary">.text-primary</h5>
  <h5 class="text-primary-emphasis">.text-primary-emphasis</h5>
  <h5 class="text-secondary">.text-secondary</h5>
  <h5 class="text-secondary-emphasis">.text-secondary-emphasis</h5>
  <h5 class="text-success">.text-success</h5>
  <h5 class="text-success-emphasis">.text-success-emphasis</h5>
  <h5 class="text-danger">.text-danger</h5>
  <h5 class="text-danger-emphasis">.text-danger-emphasis</h5>
  <h5 class="text-warning">.text-warning</h5>
  <h5 class="text-warning-emphasis">.text-warning-emphasis</h5>
  <h5 class="text-info">.text-info</h5>
  <h5 class="text-info-emphasis">.text-info-emphasis</h5>
  <h5 class="text-dark"> <span class="bg-light">.text-dark</span></h5>
  <h5 class="text-dark-emphasis">.text-dark-emphasis</h5>
  <h5 class="text-light"><span class="bg-dark">.text-light</span></h5>
  <h5 class="text-light-emphasis">.text-light-emphasis</h5><br />
  <h5 class="text-body">.text-body</h5>
  <h5 class="text-body-emphasis">.text-body-emphasis</h5>
  <h5 class="text-body-secondary">.text-body-secondary</h5>
  <h5 class="text-body-tertiary">.text-body-tertiary</h5><br />
  <h5 class="text-black"><span class="bg-light">.text-black</span></h5>
  <h5 class="text-white"> <span class="bg-dark">.text-white</span></h5>
  <h5 class="text-black-50"><span class="bg-light">.text-black-50</span></h5>
  <h5 class="text-white-50"><span class="bg-dark">.text-white-50</span></h5><br />
  <h5 class="text-1000">.text-1000</h5>
  <h5 class="text-900">.text-900</h5>
  <h5 class="text-800">.text-800</h5>
  <h5 class="text-700">.text-700</h5>
  <h5 class="text-600">.text-600</h5>
  <h5 class="text-500">.text-500</h5>
  <h5 class="text-400">.text-400</h5>
  <h5 class="text-300">.text-300</h5>
  <h5 class="text-200">.text-200</h5>
  <h5 class="text-100"><span class="bg-dark dark__bg-light">.text-100</span></h5>
</>`;
const textOpacityCode = `<>
  <h5 class="text-primary">This is default primary text</h5>
  <h5 class="text-primary text-opacity-75">This is 75% opacity primary text</h5>
  <h5 class="text-primary text-opacity-50">This is 50% opacity primary text</h5>
  <h5 class="text-primary text-opacity-25">This is 25% opacity primary text</h5>
</>`;

const Colors = () => (
  <>
    <PageHeader
      title="Colors"
      description="Convey meaning through color with a handful of color utility classes. Includes support for styling links with hover states, too."
      className="mb-3"
    />
    <FalconComponentCard>
      <FalconComponentCard.Header title="Text colors"></FalconComponentCard.Header>
      <FalconComponentCard.Body code={textColorsCode} language="jsx" />
    </FalconComponentCard>
    <FalconComponentCard noGuttersBottom>
      <FalconComponentCard.Header title="Text opacity"></FalconComponentCard.Header>
      <FalconComponentCard.Body code={textOpacityCode} language="jsx" />
    </FalconComponentCard>
  </>
);

export default Colors;
