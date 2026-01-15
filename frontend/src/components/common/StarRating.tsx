
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Rating as ReactRating, RatingProps } from 'react-simple-star-rating';

interface StarRatingProps extends RatingProps {}

const StarRating: React.FC<StarRatingProps> = ({ ...options }) => {
  return (
    <ReactRating
      allowFraction
      fillIcon={<FontAwesomeIcon icon="star" className="text-warning" />}
      emptyIcon={<FontAwesomeIcon icon="star" className="text-300" />}
      {...options}
    />
  );
};

export default StarRating;
