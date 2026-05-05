import { useEffect, CSSProperties } from 'react';
import L from 'leaflet';
import { MapContainer, TileLayer, Marker, Popup, useMap } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-markercluster';
import 'leaflet.tilelayer.colorfilter';
import 'leaflet/dist/leaflet.css';
import 'react-leaflet-markercluster/styles';
import { useAppContext } from 'providers/AppProvider';

interface MarkerData {
  id: string | number;
  lat: number;
  long: number;
  name: string;
  street: string;
  location: string;
}

interface LayerComponentProps {
  data: MarkerData[];
}

const LayerComponent = ({ data }: LayerComponentProps) => {
  const map = useMap();
  const { config } = useAppContext();

  const { isDark } = config;
  const filter = isDark
    ? [
        'invert:98%',
        'grayscale:69%',
        'bright:89%',
        'contrast:111%',
        'hue:205deg',
        'saturate:1000%'
      ]
    : ['bright:101%', 'contrast:101%', 'hue:23deg', 'saturate:225%'];

  useEffect(() => {
    map.invalidateSize();
  }, [config]);

  useEffect(() => {
    if (map) {
      L.tileLayer
        .colorFilter(
          'https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png',
          {
            attribution: null,
            transparent: true,
            filter: filter
          }
        )
        .addTo(map);
    }
  }, [isDark]);

  return (
    <>
      <TileLayer
        url={'https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png'}
      />
      <MarkerClusterGroup chunkedLoading={true} spiderfyOnMaxZoom={false}>
        {data.map((marker: MarkerData) => (
          <Marker
            key={marker.id}
            position={[marker.lat, marker.long]}
          >
            <Popup>
              <h6 className="mb-1">{marker.name}</h6>
              <p className="m-0 text-500">
                {marker.street} {marker.location}
              </p>
            </Popup>
          </Marker>
        ))}
      </MarkerClusterGroup>
    </>
  );
};

interface LeafletMapProps {
  data: MarkerData[];
  className?: string;
  style?: CSSProperties;
}

const LeafletMap = ({ data, className, style }: LeafletMapProps) => {
  const position: [number, number] = [10.737, 0];
  const {
    config: { isRTL }
  } = useAppContext();

  return (
    <MapContainer
      // @ts-expect-error - zoom, minZoom, and zoomSnap are valid MapContainer props from MapOptions
      zoom={isRTL ? 1.8 : 1.5}
      minZoom={isRTL ? 1.8 : 1.3}
      zoomSnap={0.5}
      center={position}
      className={className}
      style={style}
    >
      <LayerComponent data={data} />
    </MapContainer>
  );
};

export default LeafletMap;
