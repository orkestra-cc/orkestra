
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router';
import ReduxProvider from 'store/ReduxProvider';
import AppProvider from 'providers/AppProvider';
import { router } from 'routes/index';
import 'helpers/initFA';

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
