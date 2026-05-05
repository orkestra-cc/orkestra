---
name: react-frontend
description: Professional React frontend developer for Orkestra. Creates production-grade components following Falcon design system patterns. Use when building React components, pages, forms, or UI features.
---

This skill creates professional React frontend components that follow Orkestra's established patterns and Falcon design system.

## Technology Stack

- **React 19** with TypeScript
- **React Bootstrap 2.10** + Bootstrap 5.3
- **SCSS** with CSS custom properties
- **TanStack Query** for server state
- **Redux Toolkit** for complex shared state
- **React Hook Form** + Yup for forms

## Component Structure

### File Organization
```
src/
├── components/     # Reusable UI components
├── pages/          # Production page components
├── reference/      # Pattern library (copy patterns from here)
├── hooks/          # Custom React hooks
├── providers/      # Context providers
└── store/          # Redux store and slices
```

### Import Order
Always organize imports in this order:

```typescript
// 1. React and third-party libraries
import React, { useState, useEffect } from 'react';
import { Button, Card, Row, Col, Form } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';

// 2. Project components
import PageHeader from 'components/common/PageHeader';
import { Avatar, Flex, IconButton } from 'components/common';

// 3. Providers and hooks
import { useAppContext } from 'providers/AppProvider';
import { useForm } from 'react-hook-form';

// 4. Types and utilities
import { getColor } from 'helpers/utils';
```

### Component Pattern
```typescript
import React from 'react';
import { Card, Row, Col } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';

interface ComponentProps {
  title: string;
  children?: React.ReactNode;
}

const ComponentName: React.FC<ComponentProps> = ({ title, children }) => {
  return (
    <>
      <PageHeader title={title} className="mb-3" />
      <Card>
        <Card.Body>{children}</Card.Body>
      </Card>
    </>
  );
};

export default ComponentName;
```

## Styling Guidelines

### Use Bootstrap Utility Classes
```typescript
// Layout
<div className="d-flex align-items-center justify-content-between">
<Row className="g-3 mb-3">
<Col lg={6} md={12}>

// Spacing (margin/padding)
className="mb-3 mt-2 px-4 py-2"

// Text
className="text-primary text-700 fs-10"

// Background
className="bg-body-tertiary bg-dark"
```

### Falcon Button Variants
```typescript
<Button variant="falcon-primary">Primary Action</Button>
<Button variant="falcon-default">Default</Button>
<Button variant="falcon-success">Success</Button>
<Button variant="falcon-danger">Danger</Button>
```

### Conditional Classes
```typescript
import classNames from 'classnames';

className={classNames('base-class', {
  'active-class': isActive,
  'disabled-class': isDisabled
})}
```

### Theme Colors
Access via CSS variables or helpers:
- Primary: `--falcon-primary` (#2c7be5)
- Secondary: `--falcon-secondary` (#748194)
- Success: `--falcon-success` (#00d27a)
- Warning: `--falcon-warning` (#f5803e)
- Danger: `--falcon-danger` (#e63757)
- Info: `--falcon-info` (#27bcfd)

```typescript
import { getColor } from 'helpers/utils';
const primaryColor = getColor('primary');
```

## Common Patterns

### Page with Header
```typescript
const MyPage: React.FC = () => (
  <>
    <PageHeader
      title="Page Title"
      description="Optional description text"
      className="mb-3"
    />
    <Row className="g-3">
      <Col lg={8}>
        <Card><Card.Body>Content</Card.Body></Card>
      </Col>
    </Row>
  </>
);
```

### Form with Validation
```typescript
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';

const schema = yup.object({
  email: yup.string().email().required(),
  name: yup.string().required()
});

const MyForm: React.FC = () => {
  const { register, handleSubmit, formState: { errors } } = useForm({
    resolver: yupResolver(schema)
  });

  const onSubmit = (data: FormData) => { /* handle */ };

  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <Form.Group className="mb-3">
        <Form.Label>Email</Form.Label>
        <Form.Control
          type="email"
          isInvalid={!!errors.email}
          {...register('email')}
        />
        <Form.Control.Feedback type="invalid">
          {errors.email?.message}
        </Form.Control.Feedback>
      </Form.Group>
      <Button variant="falcon-primary" type="submit">Submit</Button>
    </Form>
  );
};
```

### Data Fetching with TanStack Query
```typescript
import { useQuery, useMutation } from '@tanstack/react-query';

const { data, isLoading, error } = useQuery({
  queryKey: ['users'],
  queryFn: async () => {
    const res = await fetch('/api/users', { credentials: 'include' });
    return res.json();
  }
});
```

### Theme Access
```typescript
import { useAppContext } from 'providers/AppProvider';

const { config: { isDark, isRTL } } = useAppContext();
```

## Tabs

**All tab components must sync active tab with URL search params** so pages are shareable and bookmarkable. Use `useSearchParams` from `react-router-dom`, never `useState`. See the `url-tabs` skill for the required pattern and examples.

## DO NOT

- Use CSS modules or styled-components
- Use generic fonts (Inter, Roboto, Arial)
- Create inline styles when Bootstrap classes exist
- Ignore dark mode compatibility
- Skip TypeScript interfaces for props
- Use class components
- Use `useState` for tab selection — tabs must sync with URL (see `url-tabs` skill)

## Reference

Before creating new components, check `/frontend-admin/src/reference/` for existing patterns:
- `reference/components/` - UI component examples
- `reference/app-examples/` - Full feature implementations
- `reference/pages/` - Page layout examples
