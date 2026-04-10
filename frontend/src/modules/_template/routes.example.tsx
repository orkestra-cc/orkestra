// Example route registration pattern.
//
// Routes are NOT defined per-module today — they all live in
// `src/routes/index.tsx` so React Router can build a single tree.
// To "add a module's routes" you add lazy imports + route objects to
// that file. This snippet shows what to add.

/*
Add near the other lazy imports at the top of src/routes/index.tsx:

  const WidgetList   = lazy(() => import('pages/widgets/list'));
  const WidgetDetail = lazy(() => import('pages/widgets/detail'));

Then add the route objects inside the protected MainLayout children
(look for the existing entries like `{ path: '/billing/dashboard', ... }`
and follow the same pattern):

  {
    path: 'widgets',
    children: [
      { index: true, element: <WidgetList /> },
      { path: ':id', element: <WidgetDetail /> },
    ],
  },

The sidebar entry comes from the BACKEND — declare it in the addon's
NavItems() method in `backend/internal/addons/widgets/module.go`. The
React app fetches the merged nav from /v1/navigation at boot via
`useRoleBasedNavigation` and renders only the items the backend reports.
*/

export {};
