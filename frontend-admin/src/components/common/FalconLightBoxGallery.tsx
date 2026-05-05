import { useState, ReactNode, Dispatch, SetStateAction } from 'react';
import Lightbox from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';

interface FalconLightBoxGalleryProps {
  images: string[];
  children: (setImgIndex: Dispatch<SetStateAction<number | null>>) => ReactNode;
}

const FalconLightBoxGallery = ({ images, children }: FalconLightBoxGalleryProps) => {
  const [imgIndex, setImgIndex] = useState<number | null>(null);
  return (
    <div>
      {children(setImgIndex)}
      {imgIndex !== null && (
        <Lightbox
          open={imgIndex !== null}
          close={() => setImgIndex(null)}
          slides={images.map((src: string) => ({ src }))}
          index={imgIndex ?? 0}
          styles={{ container: { zIndex: 999999 } }}
          on={{
            view: ({ index }: { index: number }) => {
              setImgIndex(index);
              window.dispatchEvent(new Event('resize'));
            }
          }}
        />
      )}
    </div>
  );
};

export default FalconLightBoxGallery;
