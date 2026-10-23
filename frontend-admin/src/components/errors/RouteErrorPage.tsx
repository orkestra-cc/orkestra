import { useEffect, useState } from 'react';
import { useRouteError } from 'react-router';

import OrkestraLoader from 'components/common/OrkestraLoader';
import Error500 from 'components/errors/Error500';
import ErrorLayout from 'layouts/ErrorLayout';
import {
  isStaleChunkError,
  reloadForStaleChunk,
  staleChunkReloadAllowed
} from 'helpers/staleChunk';

// The root route's error page. A lazy page left over from a previous deploy
// reloads the page instead (see helpers/staleChunk); anything else shows the
// 500 card, whose button loads the dashboard from the server so it cannot
// fall back into the same broken bundle.
const RouteErrorPage: React.FC = () => {
  const error = useRouteError();
  const [reloading] = useState(
    () => isStaleChunkError(error) && staleChunkReloadAllowed()
  );

  useEffect(() => {
    if (reloading) reloadForStaleChunk();
  }, [reloading]);

  if (reloading) return <OrkestraLoader fullPage />;

  return (
    <ErrorLayout>
      <Error500 fullPageLoad />
    </ErrorLayout>
  );
};

export default RouteErrorPage;
