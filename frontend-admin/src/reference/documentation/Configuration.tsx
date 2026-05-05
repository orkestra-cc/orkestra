
import { Card, Table } from 'react-bootstrap';
import FalconCardHeader from 'components/common/FalconCardHeader';
import FalconEditor from 'components/common/FalconEditor';
import PageHeader from 'components/common/PageHeader';
import { version } from 'config';

const configCode = `export const version = '${version}';
export const navbarBreakPoint = 'xl'; // Vertical navbar breakpoint
export const topNavbarBreakpoint = 'lg';
export const settings = {
  isFluid: false,
  isRTL: false,
  theme: 'light',
  isDark: false,
  navbarPosition: 'vertical',
  showBurgerMenu: false, // controls showing vertical nav on mobile
  currency: '$',
  isNavbarVerticalCollapsed: false, // toggle vertical navbar collapse
  navbarStyle: 'transparent'
};

export default { version, navbarBreakPoint, topNavbarBreakpoint, settings };`;

const Configuration = () => (
  <>
    <PageHeader title="Setting up configuration" className="mb-3">
      <p className="mt-2 mb-0">
        Falcon-React has a global configuration system. You can change the theme
        settings with a single global javascript object.
      </p>
    </PageHeader>

    <Card className="mb-3">
      <FalconCardHeader title="Settings configuration" />
      <Card.Body>
        <p>
          Control side panel settings from one place. Go to{' '}
          <code>src/config.js</code> file and set your setting configuration. If
          any config value is already saved in local storage and you want to
          change the default config, please clear the local storage first.
        </p>
        <FalconEditor code={configCode} language="js" hidePreview />
      </Card.Body>
    </Card>

    <Card className="mb-3">
      <FalconCardHeader title="Available Options" light={false} />
      <Card.Body className="bg-body-tertiary">
        <Table bordered responsive className="fs-10 mb-0">
          <thead className="bg-300 text-900">
            <tr>
              <th className="white-space-nowrap text-body-emphasis">Option</th>
              <th className="white-space-nowrap text-body-emphasis">Type</th>
              <th className="white-space-nowrap text-body-emphasis">
                Defaults
              </th>
              <th className="white-space-nowrap text-body-emphasis">
                Description
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="white-space-nowrap text-body">isFluid</td>
              <td className="white-space-nowrap">
                {' '}
                <code>Boolean </code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>false</code>
              </td>
              <td>
                Set <code>true</code> to enable fluid mode. This will make the
                container full-width.
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">isRTL</td>
              <td>
                <code>Boolean</code>
              </td>
              <td>
                <code>false</code>
              </td>
              <td>
                Set <code>true</code> to make the whole layout RTL (Right to
                Left). Recommended for languages such as Arabic, Hebrew, or
                other RTL languages.
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">theme</td>
              <td>
                <code>string </code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>light </code>
              </td>
              <td>
                This option is for setting up the color mode. Available values:
                <ul className="mb-0">
                  <li>
                    <code>light</code>
                  </li>
                  <li>
                    <code>dark</code>
                  </li>
                  <li>
                    <code>auto</code>
                  </li>
                </ul>
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">isDark</td>
              <td>
                <code>Boolean </code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>false </code>
              </td>
              <td>
                This option is for setting up the <code> dark </code>color mode.
                Set
                <code> true </code> to make the default color mode dark.
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">navbarPosition</td>
              <td className="white-space-nowrap">
                <code> string </code>
              </td>
              <td className="white-space-nowrap">
                <code> vertical </code>
              </td>
              <td>
                For setting the navbar position. Available values:
                <ul className="mb-0">
                  <li>
                    <code>top</code>
                  </li>
                  <li>
                    <code>vertical</code>
                  </li>
                  <li>
                    <code>combo</code>
                  </li>
                  <li>
                    <code>double-top</code>
                  </li>
                </ul>
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">showBurgerMenu</td>
              <td>
                <code>Boolean </code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>false </code>
              </td>
              <td>
                This option is for showing up the <code> vertical navbar </code>{' '}
                menus in small screens <code>(sm to lg)</code>.
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">currency</td>
              <td>
                <code>string </code>
              </td>
              <td className="white-space-nowrap">
                <code> $ </code>
              </td>
              <td>Set the currency symbole for the whole project.</td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">
                isNavbarVerticalCollapsed
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>Boolean</code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>false</code>
              </td>
              <td>
                Set <code>true</code> to make the vertical navbar stay collapsed
                when the page loads.{' '}
              </td>
            </tr>

            <tr>
              <td className="white-space-nowrap text-body">navbarStyle</td>
              <td className="white-space-nowrap">
                {' '}
                <code>string </code>
              </td>
              <td className="white-space-nowrap">
                {' '}
                <code>transparent</code>
              </td>
              <td>
                This option is for changing the vertical navigation styling.
                Available values:
                <ul className="mb-0">
                  <li>
                    {' '}
                    <code>transparent</code>
                  </li>
                  <li>
                    {' '}
                    <code>vibrant</code>
                  </li>
                  <li>
                    {' '}
                    <code>inverted</code>
                  </li>
                  <li>
                    {' '}
                    <code>card</code>
                  </li>
                </ul>
              </td>
            </tr>
          </tbody>
        </Table>
      </Card.Body>
    </Card>
  </>
);

export default Configuration;
