
import { Alert, Card } from 'react-bootstrap';
import changeLogs from './changelogs';
import createMarkup from 'helpers/createMarkup';
import SubtleBadge from 'components/common/SubtleBadge';
import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface LogsProps {
  version: string;
  warning?: { title: string; text: string; link?: string };
  title: string;
  badgeTitle?: string;
  publish: string;
  logs?: Record<string, any>;
  children?: React.ReactNode;
  index: number;
}

const Logs = ({
  version,
  warning,
  title,
  badgeTitle,
  publish,
  logs = {},
  children,
  index: currentIndex
}: LogsProps) => (
  <Card className="mb-3">
    <Card.Header>
      <h5>
        <code className="fw-bold fs-8">v{version}</code> - {title}{' '}
        {badgeTitle && (
          <SubtleBadge pill bg="warning" className="me-2">
            {badgeTitle}
          </SubtleBadge>
        )}
      </h5>
      <p className="mb-0">{publish}</p>
    </Card.Header>
    <Card.Body className="bg-body-tertiary">
      {warning && Object.keys(warning).length && (
        <Alert variant="warning" className="p-card">
          <Flex>
            <FontAwesomeIcon icon="exclamation-triangle" className="fs-6" />
            <div className="ms-3 flex-1">
              <h4 className="alert-heading">{warning.title}</h4>
              <p
                className="d-inline mb-0"
                dangerouslySetInnerHTML={{ __html: warning.text }}
              />{' '}
              {/* {warning.text}{' '} */}
              {warning.link && (
                <span>
                  click{' '}
                  <a target="_blank" rel="noreferrer" href={warning.link}>
                    here.
                  </a>
                </span>
              )}
            </div>
          </Flex>
        </Alert>
      )}
      {children}
      {Object.keys(logs).map((value, index) => {
        return value === 'Migration' ? (
          <div key={index}>
            <h5 className="fs-9">
              {value}:{' '}
              <strong>
                <code>v{changeLogs[currentIndex + 1].version}</code>
              </strong>{' '}
              to{' '}
              <strong>
                <code>v{version}</code>
              </strong>
            </h5>
            <ul className="ps-3">
              {Object.entries(logs[value] as Record<string, string[]>).map((entry, i) => (
                <li key={i}>
                  <h6>{entry[0]}</h6>
                  <ul>
                    {(entry[1] as string[]).map((mv: string, mi: number) => (
                      <li
                        dangerouslySetInnerHTML={{ __html: mv }}
                        key={entry[0] + i + mi}
                      />
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <div key={index}>
            <h5 className="fs-9">{value}</h5>
            <ul className="ps-3">
              {(logs[value] as string[]).map((v: string, i: number) => (
                <li key={i} dangerouslySetInnerHTML={createMarkup(v)} />
              ))}
            </ul>
          </div>
        );
      })}
    </Card.Body>
  </Card>
);

export default Logs;
