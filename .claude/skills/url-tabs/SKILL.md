---
name: url-tabs
description: Enforces URL-synced tabs in React frontend. Every tab component must persist active tab in URL search params so pages are shareable and bookmarkable. Use PROACTIVELY whenever creating or modifying components with tabs.
---

# URL-Synced Tabs

**Rule: Every tab component MUST sync its active tab with URL search parameters.**

Users must be able to share a URL that opens a specific tab. Local `useState` for tab selection is **forbidden** — always use `useSearchParams` from `react-router`.

## Required Pattern

```typescript
import { useSearchParams } from 'react-router';
import { Tabs, Tab } from 'react-bootstrap';

const MyPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get('tab') || 'default-tab';

  const handleTabSelect = (key: string | null) => {
    if (!key) return;
    setSearchParams((prev) => {
      prev.set('tab', key);
      return prev;
    }, { replace: true });
  };

  return (
    <Tabs activeKey={activeTab} onSelect={handleTabSelect}>
      <Tab eventKey="default-tab" title="First">
        {/* content */}
      </Tab>
      <Tab eventKey="second" title="Second">
        {/* content */}
      </Tab>
    </Tabs>
  );
};
```

### Key details

- **`replace: true`** — prevents every tab click from creating a browser history entry
- **Preserve other params** — use the callback form of `setSearchParams` so existing query params (filters, pagination) are not wiped out
- **Sensible default** — the `|| 'default-tab'` fallback ensures the page works when no `?tab=` param is present

## Tab.Container Pattern

For vertical/pill tab layouts using `Tab.Container`:

```typescript
const activeTab = searchParams.get('tab') || 'general';

<Tab.Container activeKey={activeTab} onSelect={handleTabSelect}>
  <Nav variant="pills" className="flex-column">
    <Nav.Item>
      <Nav.Link eventKey="general">General</Nav.Link>
    </Nav.Item>
    <Nav.Item>
      <Nav.Link eventKey="advanced">Advanced</Nav.Link>
    </Nav.Item>
  </Nav>
  <Tab.Content>
    <Tab.Pane eventKey="general">{/* ... */}</Tab.Pane>
    <Tab.Pane eventKey="advanced">{/* ... */}</Tab.Pane>
  </Tab.Content>
</Tab.Container>
```

## Nested Tabs

When a page has multiple independent tab groups, use **distinct parameter names**:

```typescript
const outerTab = searchParams.get('tab') || 'overview';
const innerTab = searchParams.get('subtab') || 'details';

const handleOuterSelect = (key: string | null) => {
  if (!key) return;
  setSearchParams((prev) => {
    prev.set('tab', key);
    prev.delete('subtab'); // reset inner tab when outer changes
    return prev;
  }, { replace: true });
};

const handleInnerSelect = (key: string | null) => {
  if (!key) return;
  setSearchParams((prev) => {
    prev.set('subtab', key);
    return prev;
  }, { replace: true });
};
```

## Anti-Pattern (WRONG)

```typescript
// WRONG — tab state is lost on refresh, URL is not shareable
const [tab, setTab] = useState('first');

<Tabs activeKey={tab} onSelect={(k) => setTab(k || 'first')}>
```

If you encounter this pattern in existing code while modifying a component, migrate it to use `useSearchParams`.

## DO NOT

- Use `useState` for tab selection
- Use `useNavigate` to manually build URLs for tab changes — `useSearchParams` is simpler and preserves other params
- Forget `replace: true` — without it, every tab click pollutes browser history
- Wipe other search params — always use the callback form `setSearchParams((prev) => { ... })`
