import { useEffect, useState, ReactNode } from 'react';
import googleMapStyles from 'helpers/googleMapStyles';
import {
  GoogleMap as ReactGoogleMap,
  Marker,
  InfoWindow,
  useJsApiLoader
} from '@react-google-maps/api';
import mapMarker from '../../../src/assets/img/icons/map-marker.png';
import { useAppContext } from 'providers/AppProvider';

interface GoogleMapProps {
  mapStyle: string;
  initialCenter: google.maps.LatLngLiteral;
  darkStyle?: string;
  className?: string;
  children?: ReactNode;
  [key: string]: unknown;
}

const GoogleMap = ({
  mapStyle,
  initialCenter,
  darkStyle,
  className,
  children,
  ...rest
}: GoogleMapProps) => {
  const { isLoaded } = useJsApiLoader({
    googleMapsApiKey: import.meta.env.VITE_REACT_APP_GOOGLE_API_KEY
  });

  const {
    config: { isDark }
  } = useAppContext();

  const [showInfoWindow, setShowInfoWindow] = useState(false);
  const [mapStyles, setMapStyles] = useState(mapStyle);

  const options = {
    mapTypeControl: true,
    streetViewControl: true,
    fullscreenControl: true,
    styles: googleMapStyles[mapStyles]
  };

  useEffect(() => {
    if (darkStyle && isDark) setMapStyles(darkStyle);
    else setMapStyles(mapStyle);
  }, [isDark]);

  if (!isLoaded) return <div>Loading...</div>;

  return (
    <div className={`h-100 ${className}`} {...rest}>
      <ReactGoogleMap
        zoom={18}
        options={options}
        center={initialCenter}
        mapContainerStyle={{
          width: '100%',
          height: '100%'
        }}
      >
        <Marker
          onClick={() => setShowInfoWindow(!showInfoWindow)}
          position={initialCenter}
          icon={mapMarker}
        >
          {children && showInfoWindow ? (
            <InfoWindow
              position={initialCenter}
              onCloseClick={() => setShowInfoWindow(false)}
            >
              <div>{children}</div>
            </InfoWindow>
          ) : null}
        </Marker>
      </ReactGoogleMap>
    </div>
  );
};


export default GoogleMap;
