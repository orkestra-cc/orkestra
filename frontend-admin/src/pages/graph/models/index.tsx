import { Navigate } from 'react-router';

// AI Models management has moved to admin module config
const GraphModels = () => <Navigate to="/admin/modules/aimodels" replace />;

export default GraphModels;
