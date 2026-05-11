import {
  useSensors,
  useSensor,
  MouseSensor,
  PointerSensor,
  KeyboardSensor,
  TouchSensor
} from '@dnd-kit/core';
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import Bowser from 'bowser';

export const useGetDndSensor = () => {
  const webSensor = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        delay: 250,
        distance: 0
      }
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates
    })
  );

  const mobileSensor = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: {
        distance: 8
      }
    }),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 300,
        tolerance: 10
      }
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates
    })
  );
  const browser = Bowser.getParser(window.navigator.userAgent);
  const isMobile = browser.getPlatformType() === 'mobile';

  return isMobile ? mobileSensor : webSensor;
};
