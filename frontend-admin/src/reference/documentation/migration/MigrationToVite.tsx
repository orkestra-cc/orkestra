
import PageHeader from 'components/common/PageHeader';
import { Card } from 'react-bootstrap';
import FalconEditor from 'components/common/FalconEditor';
import {
  removeCode,
  viteConfigCode,
  compileSCSSCode,
  useToggleStyleCode,
  jsonScriptCode,
  jsonScriptRemoveCode,
  renameJsToJsxCode,
  envCode,
  indexHTMLCode,
  viteInstallCode,
  paths
} from 'data/migration';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faCircleExclamation } from '@fortawesome/free-solid-svg-icons';

const MigrationToVite = () => {
  return (
    <Card>
      <Card.Header>
        <PageHeader
          title="Migration Guide from v4.7.0 to v4.8.0"
          description="This guide will help you migrate Falcon React from Create React App (CRA) to Vite."
          className="mb-3"
        />
      </Card.Header>
      <Card.Body className="" id='v4.8.0'>
        <div className="mb-3">
          <h5 className="mb-2" id="pre-requisites">
            <FontAwesomeIcon icon={faCircleExclamation} className='me-2 text-warning' />
            Prerequisites
          </h5>
          <p className='mb-2'>Before you begin, ensure you have the following installed :</p>
          <ul>
            <li>
              <code>Node.js (v16 or later)</code>
            </li>
            <li>
              <code>npm (v6 or later)</code>
            </li>
          </ul>
        </div>
        <div className="mb-3">
          <h5 className="mb-2" id="remove-dependencies">
            Step 1: Remove CRA and webpack dependencies
          </h5>
          <p className='mb-2'>
            Remove the existing CRA setup and Webpack dependencies. Run the
            following command:
          </p>
          <FalconEditor code={removeCode} language="bash" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="remove-webpack">
            Step 2: Remove <code>webpack.config.js</code>
          </h5>
          <p>
            Remove the <code>webpack.config.js</code> from the root
          </p>
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="install-vite">
            Step 3: Install Vite and related dependencies
          </h5>
          <p className='mb-2'>
            Install Vite and its related dependencies, along with the
            compile-scss dependencies, to compile SCSS into CSS by running the
            following command:
          </p>
          <FalconEditor code={viteInstallCode} language="bash" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="edit-scripts">
            Step 4: Edit scripts on <code>package.json</code>
          </h5>
          <p className='mb-2'>
            Now add the following scripts in the <code>package.json</code> file:
          </p>
          <FalconEditor code={jsonScriptCode} language="json" hidePreview />
          <p className='mb-2'>
            Remove this scripts from the <code>package.json</code> file
          </p>
          <FalconEditor
            code={jsonScriptRemoveCode}
            language="json"
            hidePreview
          />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="update-env">
            Step 5: Update The <code>.env</code> file
          </h5>
          <p className="mb-2">
            Now rename the existing <code>.env</code> variables.
          </p>
          <FalconEditor code={envCode} language="env" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="configure-vite-config">
            Step 6: Configure <code>vite.config.js</code>
          </h5>
          <p className='mb-2'>
            Next, configure the <code>vite.config.js</code> file located at the
            root of your project. This file serves as Vite's main configuration
            file and includes settings for plugins, the development server, and
            the build process. Below is a sample configuration:
          </p>
          <FalconEditor code={viteConfigCode} language="js" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="add-compile-scss">
            Step 7: Add <code>compile-scss</code> file
          </h5>
          <p className='mb-2'>
            Now create a file named <code>compile-scss.js</code> at the root of
            your project. This file is responsible for compiling SCSS files into
            CSS. Below is a sample code snippet for the{' '}
            <code>compile-scss.js</code> file:
          </p>
          <FalconEditor code={compileSCSSCode} language="js" hidePreview />
        </div>
        <div className="mb-3">
          <h5 className="mb-2" id="renaming-js-files">
            Step 8: Renaming <code>.js</code> files to <code>.jsx</code>
          </h5>
          <p className='mb-2'>
            To align with React and Vite best practices (especially for JSX
            syntax support), we are migrating all relevant <code>.js</code>{' '}
            files containing JSX code to <code>.jsx</code> extension. Follow the
            steps below to rename the files:
          </p>
          <ul>
            <li className="mb-3">
              <h5 id="add-rename-script">Add the Rename script</h5>
              <p>
                We have created a script <code>renameJsToJsx.js</code> that
                automatically renames <code>.js</code> files to{' '}
                <code>.jsx</code>{' '}
                for the provided folder path.
              </p>
              <FalconEditor
                code={renameJsToJsxCode}
                language="jsx"
                hidePreview
              />
            </li>
            <li className="mb-3">
              <h5 id="configure-rename-paths">Configure the Rename paths</h5>
              <p>
                Inside the <code>renameJsToJsx.js</code> file, you can specify
                which folders or files you want to process. Example inside
                renameJsToJsx.js. Modify the paths array according to the
                folders you want to target.
              </p>
              <FalconEditor code={paths} language="js" hidePreview />
            </li>
            <li className="mb-3">
              <h5 id="run-rename-script">Run the Rename script</h5>
              <p>
                To rename all the <code>.js</code> file that contains{' '}
                <code>.jsx</code> run the below code, This will automatically
                detect all the <code>.js</code> file rename them to{' '}
                <code>.jsx</code> preserve the folder structure.
              </p>
              <FalconEditor
                code={`node renameJsToJsx.js`}
                language="bash"
                hidePreview
              />
            </li>
            <li className="mb-3">
              <h5 id="reinstall-dependencies">Reinstall Node Modules</h5>
              <p>
                After renaming, delete <code>node_modules</code> and reinstall
                dependencies to ensure Vite picks up the new file extensions
                properly.
              </p>
              <FalconEditor
                code={`rm -rf package-lock.json node_modules && npm i`}
                language="bash"
                hidePreview
              />
            </li>
            <li>
              <h5 className="text-warning" id="important-notes">
                Important Notes:
              </h5>
              <ul>
                <li>
                  <p className="mb-1">Why this change?</p>
                  <p>
                    Vite and modern tooling treat <code>.js</code> and{' '}
                    <code>.jsx</code> files differently for HMR (Hot Module
                    Reloading) and parsing. JSX code inside <code>.js</code> may
                    cause unexpected reloads or errors. Using <code>.jsx</code>{' '}
                    allows the tooling to properly process React code without
                    full-page reloads.
                  </p>
                </li>
                <li>
                  <p className="mb-1">
                    Id a file does not contain JSX, It can remain{' '}
                    <code>.js</code>
                  </p>
                  <p>
                    This renaming script only changes files that are supposed to
                    have JSX.
                  </p>
                </li>
              </ul>
            </li>
          </ul>
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="update-index-html">
            Step 9: Update <code>index.html</code> file
          </h5>
          <p className="mb-2">
            Next move the <code>index.html</code> file from the{' '}
            <code>public</code> to the root of your project. This file is used
            to serve the application. Below is a sample <code>index.html</code>{' '}
            file:
          </p>
          <FalconEditor code={indexHTMLCode} language="html" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="edit-useToggleStyle">
            Step 10: Edit <code>useToggleStyle</code> file
          </h5>
          <p className='mb-2'>
            Now, edit the <code>useToggleStyle.jsx</code> file located in the{' '}
            <code>src/hooks</code> folder. This file is used to toggle the
            application's style. Below is a sample of the{' '}
            <code>useToggleStyle.jsx</code> file:
          </p>
          <FalconEditor code={useToggleStyleCode} language="js" hidePreview />
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="update-environment">
            Step 11: Update environment variables
          </h5>
          <p className='mb-2'>
            Next, migrate the environment variables from{' '}
            <code>process.env</code> to<code>import.meta.env</code> in the
            following components:
          </p>
          <ul>
            <li>
              <code>src/routes/index.jsx</code>
            </li>
            <li>
              <code>src/hooks/useToggleStyle.jsx</code>
            </li>
            <li>
              <code>src/components/common/TinymceEditor.jsx</code>
            </li>
            <li>
              <code>src/components/map/GoogleMap.jsx</code>
            </li>
          </ul>
        </div>

        <div className="mb-3">
          <h5 className="mb-2" id="update-bootstrap-import">
            Step 12: Update the Bootstrap Imports
          </h5>
          <p className='mb-2'>
            Now, Update the Bootstrap imports in your SCSS files. Replace the
            following import statement:
          </p>
          <ul>
            <li>
              Replace all the <code>@import '~bootstrap/...'</code> prefix to{' '}
              <code>@import '../../../node_modules/bootstrap/...';</code> in{' '}
              <code>src/assets/scss/_bootstrap.scss</code>
            </li>
            <li>
              Replace all the <code>@import '~bootstrap/...'</code> prefix to{' '}
              <code>@import '../../../node_modules/bootstrap/...';</code> in{' '}
              <code>src/assets/scss/theme.scss</code>
            </li>
            <li>
              Replace all the <code>@import '~bootstrap/...'</code> prefix to{' '}
              <code>@import '../../../node_modules/bootstrap/...';</code> in{' '}
              <code>src/assets/scss/user.scss</code>
            </li>
          </ul>
        </div>
        <div className="mb-3">
          <h5 className="mb-2" id="update-css-imports">
            Step 13: Update the css imports
          </h5>
          <p className='mb-2'>Now update the css imports in the corresponding files:</p>
          <ul>
            <li>
              <code>import 'simplebar-react/dist/simplebar.min.css'</code> in{' '}
              <code>src/App.jsx</code>
            </li>
            <li>
              <code>import 'leaflet/dist/leaflet.css'</code> in{' '}
              <code>
                src/components/dashboards/project-management/project-location/ProjectLocation.jsx
              </code>
            </li>
          </ul>
        </div>
        <div>
          <h5 className="mb-2" id="run-application">
            Step 14: Run the application
          </h5>
          <p className='mb-2'>
            Now run the following command to run the application. It will the
            code in the <code>localhost:3000</code>
          </p>
          <FalconEditor code="npm run dev" language="bash" hidePreview />
        </div>
      </Card.Body>
    </Card>
  )
}

export default MigrationToVite
