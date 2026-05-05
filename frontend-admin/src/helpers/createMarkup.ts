import DOMPurify from 'dompurify';

interface MarkupResult {
  __html: string;
}

const createMarkup = (html: string): MarkupResult => ({
  __html: DOMPurify.sanitize(html)
});

export default createMarkup;