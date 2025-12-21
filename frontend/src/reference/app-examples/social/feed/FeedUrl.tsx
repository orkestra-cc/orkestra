interface FeedUrlProps {
  imgUrl?: string;
  urlAddress: string;
  title: string;
  description?: string;
}

const FeedUrl = ({ imgUrl, urlAddress, title, description }: FeedUrlProps) => (
  <a className="text-decoration-none" href="#!">
    {!!imgUrl && <img className="img-fluid rounded" src={imgUrl} alt="" />}
    <small className="text-uppercase text-700">{urlAddress}</small>
    <h6 className="fs-9 mb-0">{title}</h6>
    {!!description && <p className="fs-10 mb-0 text-700">{description}</p>}
  </a>
);

export default FeedUrl;
