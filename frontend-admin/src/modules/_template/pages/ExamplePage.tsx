// Example list page component for a feature module.
//
// To use: copy this file to `src/pages/<name>/list/index.tsx` and adapt.
// Use the primitives in `components/common/` as building blocks — they
// are the Orkestra design-system kit and match the look of every other
// page in the app.
//
// For richer patterns (calendar, kanban, chat, email, social), copy
// from `src/reference/app-examples/` instead — those are full Orkestra
// implementations you can lift wholesale.

import { useState } from 'react';
import { Button, Card, Col, Form, Row } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import { useListWidgetsQuery } from '../api';
import ExampleCard from '../components/ExampleCard';

const ExamplePage: React.FC = () => {
  const [search, setSearch] = useState('');
  const { data, isLoading, isError, refetch } = useListWidgetsQuery({
    search: search || undefined,
    limit: 20
  });

  return (
    <>
      <PageHeader
        title="Widgets"
        description="A short description of what this module manages."
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton
            icon="plus"
            variant="primary"
            onClick={() => {
              // navigate to create page or open a modal
            }}
          >
            New widget
          </IconButton>
          <IconButton
            icon="sync"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            Refresh
          </IconButton>
        </Flex>
      </PageHeader>

      <Card className="mb-3">
        <Card.Body>
          <Form.Control
            type="search"
            placeholder="Search widgets..."
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
        </Card.Body>
      </Card>

      {isLoading && <p>Loading widgets...</p>}
      {isError && (
        <Card>
          <Card.Body>
            <p className="mb-2 text-danger">Failed to load widgets.</p>
            <Button
              size="sm"
              variant="outline-primary"
              onClick={() => refetch()}
            >
              Retry
            </Button>
          </Card.Body>
        </Card>
      )}

      {data && (
        <Row className="g-3">
          {data.widgets.map(widget => (
            <Col key={widget.uuid} md={6} lg={4}>
              <ExampleCard widget={widget} />
            </Col>
          ))}
          {data.widgets.length === 0 && (
            <Col xs={12}>
              <Card>
                <Card.Body className="text-center text-muted">
                  No widgets yet. Create one to get started.
                </Card.Body>
              </Card>
            </Col>
          )}
        </Row>
      )}
    </>
  );
};

export default ExamplePage;
