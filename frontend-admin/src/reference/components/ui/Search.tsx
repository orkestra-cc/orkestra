
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import SearchBox from 'components/navbar/top/SearchBox';
import autoCompleteInitialItem from 'data/autocomplete/autocomplete';

const Search = () => (
  <>
    <PageHeader
      title="Search"
      description="Orkestra uses <code>Fuse.js</code>  for search functionality. <code>Fuse.js</code> is a powerful, lightweight fuzzy-search library, with zero dependencies."
      className="mb-3"
    >
      <Button
        href="https://fusejs.io/"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Fuse.js Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>
    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Search Example" noPreview>
        <p className="mt-2 mb-0">
          You can find Orkestra's default searchbox component in{' '}
          <code>src/components/navbar/top/SearchBox.js</code>. And demo data for
          search box in <code>src/data/autocomplete/autocomplete.js</code>
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body>
        <SearchBox autoCompleteItem={autoCompleteInitialItem} />
      </OrkestraComponentCard.Body>
    </OrkestraComponentCard>
  </>
);

export default Search;
