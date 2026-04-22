
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router';
import ReduxProvider from 'store/ReduxProvider';
import AppProvider from 'providers/AppProvider';
import { createAppRouter } from 'routes/createRouter';
import 'helpers/initFA';
import 'helpers/echartsSetup';

const router = createAppRouter();
const container = document.getElementById('main') as HTMLElement;
const root = createRoot(container);

root.render(
  <>
    <ReduxProvider>
      <AppProvider>
        <RouterProvider router={router} />
      </AppProvider>
    </ReduxProvider>
  </>
);
