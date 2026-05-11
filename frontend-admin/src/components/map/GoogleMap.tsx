/**
 * Stub — @react-google-maps/api has been removed.
 * This placeholder keeps reference example pages compiling in dev mode.
 */
interface GoogleMapProps {
  children?: React.ReactNode;
  className?: string;
  initialCenter?: { lat: number; lng: number };
  mapStyle?: string;
  darkStyle?: string;
}

const GoogleMap = ({ children, className }: GoogleMapProps) => (
  <div className={className} style={{ background: '#e9ecef', display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: 200 }}>
    <span className="text-muted">Google Maps removed — use Leaflet</span>
    {children}
  </div>
);

export default GoogleMap;
