
import { useAppContext } from 'providers/AppProvider';
import GoogleMap from 'components/map/GoogleMap';

const MapDark = () => {
  return (
    <GoogleMap
      initialCenter={{
        lat: 23.8383608,
        lng: 90.3680554
      }}
      mapStyle="Cobalt"
      className="vh-50 rounded-soft mt-5"
    />
  );
};
const MapLight = () => {
  return (
    <GoogleMap
      initialCenter={{
        lat: 23.8383608,
        lng: 90.3680554
      }}
      mapStyle="Default"
      className="vh-50 rounded-soft mt-5"
    ></GoogleMap>
  );
};
const EventDetailsGmap = () => {
  const {
    config: { isDark }
  } = useAppContext();

  return <>{isDark ? <MapDark /> : <MapLight />}</>;
};

export default EventDetailsGmap;
