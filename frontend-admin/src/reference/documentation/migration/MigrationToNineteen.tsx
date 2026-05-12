
import PageHeader from 'components/common/PageHeader';
import { Card, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faCircleInfo } from '@fortawesome/free-solid-svg-icons';
import OrkestraEditor from 'components/common/OrkestraEditor';
import { contextProviderCode, forwardRefCode, useContextCode } from 'data/migration';

const MigrationToNineteen = () => {
  return (
    <Card className='mb-3'>
      <Card.Header>
        <PageHeader
          title="Migration Guide from v4.8.0 to v5.0.0"
          description="This guide will help you migrate Orkestra React from React 18 to React 19."
          className="mb-3"
        />
      </Card.Header>
      <Card.Body className="mb-3">
        <div className='mb-3'>
          <Alert variant='warning' className="d-flex">
            <FontAwesomeIcon icon={faCircleInfo} className='text-warning fs-5 me-3' />
            <p>
              This is a major update. Please backup your project before upgrading
              to the latest version. This version is purely focused on updating
              the react version. See the{' '}
              <a
                href="https://react.dev/blog/2024/04/25/react-19-upgrade-guide"
                target="_blank"
              >
                migration guide
              </a>
            </p>
          </Alert>
        </div>
        <p>In this version, we have migrated our project from <code>React 18.3.1</code> to{' '}
          <code>React 19.1.0</code> and all the dependencies that are compatible with {' '}
          <code>React 19</code>. If you're upgrading from <code>4.8.0</code> to <code>5.0.0</code>,
          please follow the steps outlined below:
        </p>
        <div className='mb-3'>
          <h5 className="mb-2" id="v5-0-package-json-update">
            Step 1: Update the <code>package.json</code>
          </h5>
          <p>
            All Orkestra React packages are compatible with React 19. Please update all the dependencies {' '}
            and devDependencies in your <code>package.json</code> to the latest version from Orkestra React.
          </p>
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-react-router'>
            Step 2: Updating the <code>react-router-dom</code> to <code>react-router</code>. See the {' '}
            <a href="https://reactrouter.com/upgrading/v6#upgrade-to-v7" target='_blank'>migration</a>{' '}
            guide for more details.
          </h5>
          <p>
            As <code>react-router-dom</code> has updated its package name from <code>react-router-dom</code> {' '}
            to <code>react-router</code>. We have replaced all the occurrences of <code>react-router-dom</code> {' '}
            with <code>react-router</code>. So you need to update the imports in the components accordingly.
          </p>
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-suspense-boundary'>
            Step 3: Updating the <code>Suspense</code> in <code>route/index.js</code>
          </h5>
          <p>
            In the <code>routes/index.js</code> where we have used suspense, you should use the <code>key</code> property.
            {' '} It helps with better page transitions that avoid hiding already visible content.
          </p>
          <OrkestraEditor
            code={`<Suspense key={location.pathname} fallback={'loading...'}>\n {Your code...}\n</Suspense>`}
            language='jsx'
            hidePreview
          />
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-codemods'>
            Step 4: Updating the files using codemods.(Optional)
          </h5>
          <p className='mb-2'>
            React 19 provides a codemod command for converting code to be compatible with {' '}
            react 19. There are multiple command that can be run to update the code based on {' '}
            their target. For updating the forwardRef, useContext and context provider, you can run the {' '}
            following commands:
          </p>
          <OrkestraEditor
            code={`npx codemod react/19/remove-forward-ref --target src/ \nnpx codemod react/19/use-context-hook --target src/ \nnpx codemod react/19/remove-context-provider --target src/ `}
            language='bash'
            hidePreview
          />
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-forward-ref'>
            Step 5: Updating the <code>forwardRef</code>
          </h5>
          <p className='mb-2'>
            If you don't want to use the codemod commands described in step 4. You can do it manually.
            {' '} In the current version of react, you can use <code>ref</code> directly in
            the component. You need to update the components that use <code>forwardRef</code> to reference.
          </p>
          <OrkestraEditor
            code={forwardRefCode}
            language='jsx'
            hidePreview
          />
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-useContext'>
            Step 6: Switch from <code>useContext</code> to the new <code>use()</code> API hook
          </h5>
          <p className='mb-2'>
            React 19 introduces <code>use()</code> API hook, which is useful for retrieving Context or {' '}
            async data. Now look for all occurrence of <code>useContext</code> and replace them with
            {' '} <code>use()</code> hook.
          </p>
          <OrkestraEditor
            code={useContextCode}
            language='jsx'
            hidePreview
          />
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-provider'>
            Step 7: Use the shorthand <code>Provider</code> syntax
          </h5>
          <p className='mb-2'>
            In React 19, you can render <code>Context</code> as a provider instead of <code>{'<Context.Provider></Context.Provider>'}.</code>
            {' '} So replace all the occurrences of <code>{'<Context.Provider></Context.Provider>'}</code> with the shorthand
            {' '} <code>{'<Context></Context>'}</code> syntax.
          </p>
          <OrkestraEditor
            code={contextProviderCode}
            language='jsx'
            hidePreview
          />
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-rm-prop-type'>
            Step 8: Remove the <code>prop-types</code> from the file
          </h5>
          <p className='mb-2'>
            In React 19, prop-types has been officially deprecated. As a result, React 19 ignores all the type
            {' '} checking and warning for prop-types. Therefore, we have removed the <code>prop-types</code> from
            {' '}our project code.
          </p>
        </div>
        <div className='mb-3'>
          <h5 className='mb-2' id='v5-0-rm-node'>
            Step 9: Remove the <code>package-lock.json</code> and <code>node_modules</code>
          </h5>
          <p className='mb-2'>
            After replacing the <code>package.json</code> file, remove the existing <code>package-lock.json</code>
            {' '} and the <code>node_modules</code>. Then run the install command to add the dependencies.
          </p>
          <OrkestraEditor
            code={`rm -rf package-lock.json node_modules && npm i`}
            language='bash'
            hidePreview
          />
        </div>
        <div className=''>
          <h5 className='mb-2' id='v5-0-run-project'>Step 10: Run the project.</h5>
          <p className='mb-2'>
            After updating all the changes. Run the projecct to see if everything works as expected.
          </p>
          <OrkestraEditor
            code={`npm run dev`}
            language='bash'
            hidePreview
          />
        </div>
      </Card.Body>
    </Card>
  )
}

export default MigrationToNineteen
