// Tags admin — flat list with create/edit/delete. Phase 1 ships the
// minimal slug-stable CRUD surface; tree visualisation and
// move-subtree arrive in a follow-up alongside the cascade on
// tags[] arrays of Person/Organization records.

import { useState } from 'react';
import {
  Button,
  Card,
  Form,
  Modal,
  Table,
  Badge,
  InputGroup
} from 'react-bootstrap';
import {
  useListMarketingTagsQuery,
  useCreateMarketingTagMutation,
  useUpdateMarketingTagMutation,
  useDeleteMarketingTagMutation
} from 'store/api/marketingApi';
import type { Tag, TagPayload } from 'types/marketing';

const empty: TagPayload = {
  name: '',
  slug: '',
  description: '',
  color: '',
  parentUuid: ''
};

const TagsPage: React.FC = () => {
  const { data, isLoading } = useListMarketingTagsQuery();
  const [createTag] = useCreateMarketingTagMutation();
  const [updateTag] = useUpdateMarketingTagMutation();
  const [deleteTag] = useDeleteMarketingTagMutation();

  const [show, setShow] = useState(false);
  const [editing, setEditing] = useState<Tag | null>(null);
  const [form, setForm] = useState<TagPayload>(empty);

  const openNew = () => {
    setEditing(null);
    setForm(empty);
    setShow(true);
  };
  const openEdit = (t: Tag) => {
    setEditing(t);
    setForm({
      name: t.name,
      slug: t.slug,
      description: t.description ?? '',
      color: t.color ?? '',
      parentUuid: t.parentUuid ?? ''
    });
    setShow(true);
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const payload: TagPayload = {
      ...form,
      parentUuid: form.parentUuid || undefined,
      slug: form.slug || undefined
    };
    if (editing) {
      await updateTag({
        id: editing.uuid,
        patch: payload as unknown as Record<string, unknown>
      });
    } else {
      await createTag(payload);
    }
    setShow(false);
  };

  const onDelete = async (t: Tag) => {
    if (
      !window.confirm(
        `Delete tag "${t.name}"? References on contacts will become orphans.`
      )
    ) {
      return;
    }
    await deleteTag(t.uuid);
  };

  const parents = (data?.items ?? []).filter(t => !t.parentUuid);

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">Tags</h3>
          <p className="fs-10 text-muted mb-0">
            Hierarchical labels applied to persons and organizations. Slug is
            the stable machine identifier — name is free to rename without
            touching the contacts that reference it.
          </p>
        </div>
        <Button variant="primary" onClick={openNew}>
          New tag
        </Button>
      </div>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-3 text-muted">Loading…</div>
          ) : !data?.items?.length ? (
            <div className="p-3 text-muted">
              No tags yet. Operators build their own taxonomy — e.g.
              <code>/Industry</code>, <code>/Region</code>,<code>/Segment</code>{' '}
              root branches.
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>Name</th>
                  <th>Slug</th>
                  <th>Path</th>
                  <th>Color</th>
                  <th style={{ width: 140 }} />
                </tr>
              </thead>
              <tbody>
                {data.items.map(t => (
                  <tr key={t.uuid}>
                    <td className="fw-medium">{t.name}</td>
                    <td>
                      <code className="fs-10">{t.slug}</code>
                    </td>
                    <td>
                      <small className="text-muted">{t.path}</small>
                    </td>
                    <td>
                      {t.color ? (
                        <Badge
                          pill
                          style={{
                            backgroundColor: t.color,
                            color: '#fff'
                          }}
                        >
                          {t.color}
                        </Badge>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="text-end">
                      <Button
                        size="sm"
                        variant="link"
                        onClick={() => openEdit(t)}
                      >
                        Edit
                      </Button>
                      <Button
                        size="sm"
                        variant="link"
                        className="text-danger"
                        onClick={() => onDelete(t)}
                      >
                        Delete
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <Modal show={show} onHide={() => setShow(false)}>
        <Form onSubmit={onSubmit}>
          <Modal.Header closeButton>
            <Modal.Title>{editing ? 'Edit tag' : 'New tag'}</Modal.Title>
          </Modal.Header>
          <Modal.Body>
            <Form.Group className="mb-3">
              <Form.Label>Name</Form.Label>
              <Form.Control
                required
                value={form.name}
                onChange={e => setForm({ ...form, name: e.target.value })}
              />
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label>Slug</Form.Label>
              <InputGroup>
                <Form.Control
                  value={form.slug}
                  onChange={e => setForm({ ...form, slug: e.target.value })}
                  placeholder="Auto-derived from name when empty"
                />
              </InputGroup>
              <Form.Text className="text-muted">
                Empty = derived from name. Modifiable but stable — used as the
                dedup key for re-imports.
              </Form.Text>
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label>Description</Form.Label>
              <Form.Control
                as="textarea"
                rows={2}
                value={form.description ?? ''}
                onChange={e =>
                  setForm({ ...form, description: e.target.value })
                }
              />
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label>Color</Form.Label>
              <Form.Control
                type="color"
                value={form.color || '#1f6feb'}
                onChange={e => setForm({ ...form, color: e.target.value })}
                style={{ width: 80, height: 40 }}
              />
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label>Parent</Form.Label>
              <Form.Select
                value={form.parentUuid ?? ''}
                onChange={e => setForm({ ...form, parentUuid: e.target.value })}
                disabled={!!editing}
              >
                <option value="">(root)</option>
                {parents.map(p => (
                  <option key={p.uuid} value={p.uuid}>
                    {p.name} ({p.slug})
                  </option>
                ))}
              </Form.Select>
              {editing && (
                <Form.Text className="text-muted">
                  Use the (future) move-subtree action to reparent — generic
                  edit doesn't change parentUuid.
                </Form.Text>
              )}
            </Form.Group>
          </Modal.Body>
          <Modal.Footer>
            <Button variant="outline-secondary" onClick={() => setShow(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary">
              {editing ? 'Save' : 'Create'}
            </Button>
          </Modal.Footer>
        </Form>
      </Modal>
    </>
  );
};

export default TagsPage;
