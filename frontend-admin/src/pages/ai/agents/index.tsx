import { useState, useCallback, useEffect } from 'react';
import { Link } from 'react-router-dom';
import {
  Row,
  Col,
  Card,
  Table,
  Modal,
  Form,
  Button,
  Badge,
  Dropdown,
  Spinner,
  Offcanvas
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPlus,
  faEdit,
  faTrash,
  faRobot,
  faFilter,
  faEllipsisV,
  faExclamationTriangle,
  faFileAlt,
  faSlidersH
} from '@fortawesome/free-solid-svg-icons';
import { Trans, useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useListProjectsQuery,
  useCreateProjectMutation,
  useUpdateProjectMutation,
  useDeleteProjectMutation,
  useAddProjectDocumentsMutation,
  useRemoveProjectDocumentsMutation,
  useGetProjectSettingsQuery,
  useUpdateProjectSettingsMutation
} from '../../../store/api/agentsApi';
import { useListDocumentsQuery } from '../../../store/api/ragApi';
import type {
  AgentProject,
  AgentSettings,
  CreateProjectRequest,
  UpdateProjectRequest
} from '../../../types/agents';

// ---------------------------------------------------------------------------
// Greetings Banner
// ---------------------------------------------------------------------------

function AgentProjectsGreetings() {
  const { t } = useTranslation();
  return (
    <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
      <Background image={greetingsBg} className="bg-card d-none d-sm-block" />
      <Card.Header className="d-flex align-items-center z-1 p-0">
        <div className="bg-primary rounded-circle p-3 ms-3">
          <FontAwesomeIcon icon={faRobot} className="text-white" size="2x" />
        </div>
        <div className="ms-3">
          <h6 className="mb-1 text-primary">
            {t('aiAgents.greetings.kicker')}
          </h6>
          <h4 className="mb-0 text-primary fw-bold">
            {t('aiAgents.greetings.title')}
            <span className="text-info fw-medium">
              {t('aiAgents.greetings.titleAccent')}
            </span>
          </h4>
          <p className="mb-0 mt-1 text-muted small">
            {t('aiAgents.greetings.subtitle')}
          </p>
        </div>
      </Card.Header>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Create / Edit Modal
// ---------------------------------------------------------------------------

interface ProjectFormModalProps {
  show: boolean;
  onHide: () => void;
  editingProject: AgentProject | null;
}

function ProjectFormModal({
  show,
  onHide,
  editingProject
}: ProjectFormModalProps) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [status, setStatus] = useState<'active' | 'archived'>('active');

  const [createProject, { isLoading: creating }] = useCreateProjectMutation();
  const [updateProject, { isLoading: updating }] = useUpdateProjectMutation();

  const isEditing = !!editingProject;
  const saving = creating || updating;

  const handleEnter = () => {
    if (editingProject) {
      setName(editingProject.name);
      setDescription(editingProject.description);
      setStatus(editingProject.status);
    } else {
      setName('');
      setDescription('');
      setStatus('active');
    }
  };

  const handleSave = useCallback(async () => {
    try {
      if (editingProject) {
        const body: UpdateProjectRequest = {};
        if (name !== editingProject.name) body.name = name;
        if (description !== editingProject.description)
          body.description = description;
        if (status !== editingProject.status) body.status = status;
        await updateProject({ uuid: editingProject.uuid, body }).unwrap();
      } else {
        const body: CreateProjectRequest = { name, description };
        await createProject(body).unwrap();
      }
      onHide();
    } catch {
      // Handled by RTK Query
    }
  }, [
    createProject,
    updateProject,
    editingProject,
    name,
    description,
    status,
    onHide
  ]);

  return (
    <Modal show={show} onHide={onHide} onEnter={handleEnter}>
      <Modal.Header closeButton>
        <Modal.Title>
          {isEditing
            ? t('aiAgents.projectForm.titleEdit')
            : t('aiAgents.projectForm.titleCreate')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('aiAgents.projectForm.nameLabel')}
          </Form.Label>
          <Form.Control
            size="sm"
            value={name}
            onChange={e => setName(e.target.value)}
            placeholder={t('aiAgents.projectForm.namePlaceholder')}
          />
        </Form.Group>
        <Form.Group className="mb-3">
          <Form.Label className="small">
            {t('aiAgents.projectForm.descriptionLabel')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={3}
            size="sm"
            value={description}
            onChange={e => setDescription(e.target.value)}
            placeholder={t('aiAgents.projectForm.descriptionPlaceholder')}
          />
        </Form.Group>
        {isEditing && (
          <Form.Group>
            <Form.Label className="small">
              {t('aiAgents.projectForm.statusLabel')}
            </Form.Label>
            <Form.Select
              size="sm"
              value={status}
              onChange={e => setStatus(e.target.value as 'active' | 'archived')}
            >
              <option value="active">
                {t('aiAgents.projectForm.statusActive')}
              </option>
              <option value="archived">
                {t('aiAgents.projectForm.statusArchived')}
              </option>
            </Form.Select>
          </Form.Group>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={onHide}>
          {t('aiAgents.projectForm.cancel')}
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={handleSave}
          disabled={saving || !name.trim() || !description.trim()}
        >
          {saving ? (
            <Spinner size="sm" />
          ) : isEditing ? (
            t('aiAgents.projectForm.saveChanges')
          ) : (
            t('aiAgents.projectForm.create')
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Delete Confirmation Modal
// ---------------------------------------------------------------------------

interface DeleteProjectModalProps {
  show: boolean;
  onHide: () => void;
  project: AgentProject | null;
}

function DeleteProjectModal({
  show,
  onHide,
  project
}: DeleteProjectModalProps) {
  const { t } = useTranslation();
  const [deleteProject, { isLoading }] = useDeleteProjectMutation();

  const handleConfirm = useCallback(async () => {
    if (!project) return;
    try {
      await deleteProject(project.uuid).unwrap();
      onHide();
    } catch {
      // Handled by RTK Query
    }
  }, [deleteProject, project, onHide]);

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title className="text-danger">
          <FontAwesomeIcon icon={faExclamationTriangle} className="me-2" />
          {t('aiAgents.deleteModal.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <p>
          <Trans
            i18nKey="aiAgents.deleteModal.body"
            values={{ name: project?.name }}
            components={{ strong: <strong /> }}
          />
          <br />
          <span className="text-muted">
            {t('aiAgents.deleteModal.warning')}
          </span>
        </p>
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          size="sm"
          onClick={onHide}
          disabled={isLoading}
        >
          {t('aiAgents.deleteModal.cancel')}
        </Button>
        <Button
          variant="danger"
          size="sm"
          onClick={handleConfirm}
          disabled={isLoading}
        >
          {isLoading ? <Spinner size="sm" className="me-1" /> : null}
          {t('aiAgents.deleteModal.confirm')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Manage Documents Modal
// ---------------------------------------------------------------------------

interface ManageDocumentsModalProps {
  show: boolean;
  onHide: () => void;
  project: AgentProject | null;
}

function ManageDocumentsModal({
  show,
  onHide,
  project
}: ManageDocumentsModalProps) {
  const { t } = useTranslation();
  const { data: ragData, isLoading: loadingDocs } = useListDocumentsQuery(
    { status: 'completed' },
    { skip: !show }
  );
  const [addDocuments, { isLoading: adding }] =
    useAddProjectDocumentsMutation();
  const [removeDocuments, { isLoading: removing }] =
    useRemoveProjectDocumentsMutation();

  const allDocs = ragData?.documents ?? [];
  const assignedIds = new Set(project?.documentUuids ?? []);
  const saving = adding || removing;

  const handleToggle = useCallback(
    async (docUuid: string, isCurrentlyAssigned: boolean) => {
      if (!project) return;
      try {
        if (isCurrentlyAssigned) {
          await removeDocuments({
            uuid: project.uuid,
            documentUuids: [docUuid]
          }).unwrap();
        } else {
          await addDocuments({
            uuid: project.uuid,
            documentUuids: [docUuid]
          }).unwrap();
        }
      } catch {
        // Handled by RTK Query
      }
    },
    [project, addDocuments, removeDocuments]
  );

  return (
    <Modal show={show} onHide={onHide} size="lg">
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon={faFileAlt} className="me-2 text-primary" />
          {t('aiAgents.documentsModal.title', { name: project?.name ?? '' })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body style={{ maxHeight: 400, overflowY: 'auto' }}>
        {loadingDocs ? (
          <div className="text-center py-4">
            <Spinner size="sm" />
            <span className="ms-2 text-muted">
              {t('aiAgents.documentsModal.loading')}
            </span>
          </div>
        ) : allDocs.length === 0 ? (
          <p className="text-muted text-center py-3">
            {t('aiAgents.documentsModal.empty')}
          </p>
        ) : (
          <Table hover size="sm" className="mb-0">
            <thead className="bg-body-tertiary">
              <tr>
                <th style={{ width: 40 }}></th>
                <th>{t('aiAgents.documentsModal.colDocument')}</th>
                <th>{t('aiAgents.documentsModal.colStandard')}</th>
                <th>{t('aiAgents.documentsModal.colChunks')}</th>
              </tr>
            </thead>
            <tbody>
              {allDocs.map(doc => {
                const assigned = assignedIds.has(doc.uuid);
                return (
                  <tr
                    key={doc.uuid}
                    className={assigned ? 'table-primary bg-opacity-10' : ''}
                    style={{ cursor: 'pointer' }}
                    onClick={() => handleToggle(doc.uuid, assigned)}
                  >
                    <td className="text-center">
                      <Form.Check
                        type="checkbox"
                        checked={assigned}
                        disabled={saving}
                        onChange={() => handleToggle(doc.uuid, assigned)}
                        onClick={e => e.stopPropagation()}
                      />
                    </td>
                    <td className="fw-semibold">{doc.title}</td>
                    <td className="text-muted small">
                      {doc.isoStandard || t('aiAgents.documentsModal.dash')}
                    </td>
                    <td>
                      <Badge bg="secondary" className="bg-opacity-25 text-body">
                        {doc.chunkCount}
                      </Badge>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
      </Modal.Body>
      <Modal.Footer>
        <span className="me-auto text-muted small">
          {t('aiAgents.documentsModal.assignedCount', {
            count: assignedIds.size
          })}
        </span>
        <Button variant="primary" size="sm" onClick={onHide}>
          {t('aiAgents.documentsModal.done')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Agent Settings Panel (Offcanvas)
// ---------------------------------------------------------------------------

interface SettingsPanelProps {
  show: boolean;
  onHide: () => void;
  project: AgentProject | null;
}

function SettingsPanel({ show, onHide, project }: SettingsPanelProps) {
  const { t } = useTranslation();

  const dispositionLabels: Record<string, { low: string; high: string }> = {
    skepticism: {
      low: t('aiAgents.settings.dispositionLabels.skepticismLow'),
      high: t('aiAgents.settings.dispositionLabels.skepticismHigh')
    },
    literalism: {
      low: t('aiAgents.settings.dispositionLabels.literalismLow'),
      high: t('aiAgents.settings.dispositionLabels.literalismHigh')
    },
    empathy: {
      low: t('aiAgents.settings.dispositionLabels.empathyLow'),
      high: t('aiAgents.settings.dispositionLabels.empathyHigh')
    }
  };

  const { data } = useGetProjectSettingsQuery(project?.uuid ?? '', {
    skip: !show || !project
  });
  const [updateSettings, { isLoading: saving }] =
    useUpdateProjectSettingsMutation();

  const [systemPrompt, setSystemPrompt] = useState('');
  const [directives, setDirectives] = useState('');
  const [skepticism, setSkepticism] = useState(0);
  const [literalism, setLiteralism] = useState(0);
  const [empathy, setEmpathy] = useState(0);
  const [temperature, setTemperature] = useState('');
  const [language, setLanguage] = useState('');

  useEffect(() => {
    if (!data?.settings) {
      setSystemPrompt('');
      setDirectives('');
      setSkepticism(0);
      setLiteralism(0);
      setEmpathy(0);
      setTemperature('');
      setLanguage('');
      return;
    }
    const s = data.settings;
    setSystemPrompt(s.systemPrompt || '');
    setDirectives((s.directives || []).join('\n'));
    setSkepticism(s.skepticism || 0);
    setLiteralism(s.literalism || 0);
    setEmpathy(s.empathy || 0);
    setTemperature(s.temperature || '');
    setLanguage(s.language || '');
  }, [data]);

  const handleSave = useCallback(async () => {
    if (!project) return;
    const settings: Partial<AgentSettings> = {
      systemPrompt: systemPrompt || undefined,
      directives: directives.trim()
        ? directives
            .split('\n')
            .map(d => d.trim())
            .filter(Boolean)
        : undefined,
      skepticism: skepticism || undefined,
      literalism: literalism || undefined,
      empathy: empathy || undefined,
      temperature: (temperature as AgentSettings['temperature']) || undefined,
      language: language || undefined
    };
    try {
      await updateSettings({ uuid: project.uuid, settings }).unwrap();
      onHide();
    } catch {
      // Handled by RTK Query
    }
  }, [
    updateSettings,
    project,
    onHide,
    systemPrompt,
    directives,
    skepticism,
    literalism,
    empathy,
    temperature,
    language
  ]);

  const renderSlider = (
    label: string,
    value: number,
    setter: (v: number) => void,
    key: string
  ) => (
    <Form.Group className="mb-3" key={key}>
      <div className="d-flex justify-content-between">
        <Form.Label className="small mb-1">{label}</Form.Label>
        <small className="text-muted">
          {value === 0 ? t('aiAgents.settings.sliderDefault') : value}
        </small>
      </div>
      <Form.Range
        min={0}
        max={5}
        step={1}
        value={value}
        onChange={e => setter(Number(e.target.value))}
      />
      <div className="d-flex justify-content-between">
        <small className="text-muted">{dispositionLabels[key]?.low}</small>
        <small className="text-muted">{dispositionLabels[key]?.high}</small>
      </div>
    </Form.Group>
  );

  return (
    <Offcanvas
      show={show}
      onHide={onHide}
      placement="end"
      style={{ width: 380 }}
    >
      <Offcanvas.Header closeButton>
        <Offcanvas.Title>
          <FontAwesomeIcon icon={faSlidersH} className="me-2" />
          {t('aiAgents.settings.title', { name: project?.name ?? '' })}
        </Offcanvas.Title>
      </Offcanvas.Header>
      <Offcanvas.Body>
        <Form.Group className="mb-3">
          <Form.Label className="small fw-semibold">
            {t('aiAgents.settings.systemPromptLabel')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={3}
            size="sm"
            value={systemPrompt}
            onChange={e => setSystemPrompt(e.target.value)}
            placeholder={t('aiAgents.settings.systemPromptPlaceholder')}
          />
          <Form.Text className="text-muted">
            {t('aiAgents.settings.systemPromptHelp')}
          </Form.Text>
        </Form.Group>

        <Form.Group className="mb-3">
          <Form.Label className="small fw-semibold">
            {t('aiAgents.settings.directivesLabel')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={3}
            size="sm"
            value={directives}
            onChange={e => setDirectives(e.target.value)}
            placeholder={t('aiAgents.settings.directivesPlaceholder')}
          />
          <Form.Text className="text-muted">
            {t('aiAgents.settings.directivesHelp')}
          </Form.Text>
        </Form.Group>

        <hr />
        <p className="small fw-semibold mb-2">
          {t('aiAgents.settings.dispositionHeading')}
        </p>
        {renderSlider(
          t('aiAgents.settings.sliderSkepticism'),
          skepticism,
          setSkepticism,
          'skepticism'
        )}
        {renderSlider(
          t('aiAgents.settings.sliderLiteralism'),
          literalism,
          setLiteralism,
          'literalism'
        )}
        {renderSlider(
          t('aiAgents.settings.sliderEmpathy'),
          empathy,
          setEmpathy,
          'empathy'
        )}

        <hr />
        <Form.Group className="mb-3">
          <Form.Label className="small fw-semibold">
            {t('aiAgents.settings.temperatureLabel')}
          </Form.Label>
          <Form.Select
            size="sm"
            value={temperature}
            onChange={e => setTemperature(e.target.value)}
          >
            <option value="">
              {t('aiAgents.settings.temperaturePersona')}
            </option>
            <option value="precise">
              {t('aiAgents.settings.temperaturePrecise')}
            </option>
            <option value="balanced">
              {t('aiAgents.settings.temperatureBalanced')}
            </option>
            <option value="creative">
              {t('aiAgents.settings.temperatureCreative')}
            </option>
          </Form.Select>
        </Form.Group>

        <Form.Group className="mb-3">
          <Form.Label className="small fw-semibold">
            {t('aiAgents.settings.languageLabel')}
          </Form.Label>
          <Form.Select
            size="sm"
            value={language}
            onChange={e => setLanguage(e.target.value)}
          >
            <option value="">{t('aiAgents.settings.languageAuto')}</option>
            <option value="en">{t('aiAgents.settings.languageEn')}</option>
            <option value="it">{t('aiAgents.settings.languageIt')}</option>
            <option value="es">{t('aiAgents.settings.languageEs')}</option>
            <option value="de">{t('aiAgents.settings.languageDe')}</option>
            <option value="fr">{t('aiAgents.settings.languageFr')}</option>
          </Form.Select>
        </Form.Group>

        <Button
          variant="primary"
          className="w-100"
          onClick={handleSave}
          disabled={saving}
        >
          {saving ? <Spinner size="sm" className="me-1" /> : null}
          {t('aiAgents.settings.save')}
        </Button>
      </Offcanvas.Body>
    </Offcanvas>
  );
}

// ---------------------------------------------------------------------------
// Projects Table
// ---------------------------------------------------------------------------

function ProjectsTable() {
  const { t } = useTranslation();
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [showFormModal, setShowFormModal] = useState(false);
  const [editingProject, setEditingProject] = useState<AgentProject | null>(
    null
  );
  const [deletingProject, setDeletingProject] = useState<AgentProject | null>(
    null
  );
  const [docsProjectUuid, setDocsProjectUuid] = useState<string | null>(null);
  const [settingsProject, setSettingsProject] = useState<AgentProject | null>(
    null
  );

  const queryParams = statusFilter ? { status: statusFilter } : undefined;
  const { data, isLoading } = useListProjectsQuery(queryParams);

  const projects = data?.projects ?? [];

  const openCreate = () => {
    setEditingProject(null);
    setShowFormModal(true);
  };

  const openEdit = (project: AgentProject) => {
    setEditingProject(project);
    setShowFormModal(true);
  };

  const closeFormModal = () => {
    setShowFormModal(false);
    setEditingProject(null);
  };

  const truncate = (text: string, maxLength: number) => {
    if (text.length <= maxLength) return text;
    return text.slice(0, maxLength) + '...';
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-GB', {
      day: '2-digit',
      month: 'short',
      year: 'numeric'
    });
  };

  return (
    <>
      <Card>
        <Card.Header className="border-bottom border-200">
          <div className="d-flex align-items-center justify-content-between flex-wrap gap-2">
            <div className="d-flex gap-2 align-items-center">
              <FontAwesomeIcon icon={faFilter} className="text-muted" />
              <Form.Select
                size="sm"
                value={statusFilter}
                onChange={e => setStatusFilter(e.target.value)}
                style={{ width: 150 }}
              >
                <option value="">{t('aiAgents.table.filterAll')}</option>
                <option value="active">
                  {t('aiAgents.table.filterActive')}
                </option>
                <option value="archived">
                  {t('aiAgents.table.filterArchived')}
                </option>
              </Form.Select>
            </div>
            <Button size="sm" variant="primary" onClick={openCreate}>
              <FontAwesomeIcon icon={faPlus} className="me-1" />
              {t('aiAgents.table.newProject')}
            </Button>
          </div>
        </Card.Header>

        <Card.Body className="p-0">
          {isLoading ? (
            <div className="text-center py-5">
              <Spinner animation="border" variant="primary" />
              <p className="mt-2 text-muted">
                {t('aiAgents.table.loadingProjects')}
              </p>
            </div>
          ) : projects.length === 0 ? (
            <div className="text-center py-5">
              <FontAwesomeIcon
                icon={faRobot}
                className="text-muted mb-3"
                size="3x"
              />
              <p className="text-muted">{t('aiAgents.table.empty')}</p>
              <Button variant="outline-primary" size="sm" onClick={openCreate}>
                {t('aiAgents.table.createFirst')}
              </Button>
            </div>
          ) : (
            <div className="table-responsive">
              <Table hover className="mb-0">
                <thead className="bg-body-tertiary">
                  <tr>
                    <th>{t('aiAgents.table.colName')}</th>
                    <th>{t('aiAgents.table.colDescription')}</th>
                    <th>{t('aiAgents.table.colStatus')}</th>
                    <th>{t('aiAgents.table.colDocuments')}</th>
                    <th>{t('aiAgents.table.colCreated')}</th>
                    <th className="text-end">
                      {t('aiAgents.table.colActions')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {projects.map(project => (
                    <tr key={project.uuid}>
                      <td className="fw-semibold">
                        <Link
                          to={`/ai/agents/${project.uuid}/chat`}
                          className="text-decoration-none"
                        >
                          {project.name}
                        </Link>
                      </td>
                      <td
                        className="small text-muted"
                        style={{ maxWidth: 300 }}
                        title={project.description}
                      >
                        {truncate(project.description, 80)}
                      </td>
                      <td>
                        <Badge
                          bg={
                            project.status === 'active'
                              ? 'success'
                              : 'secondary'
                          }
                          className="bg-opacity-25 text-body"
                        >
                          {project.status}
                        </Badge>
                      </td>
                      <td>
                        <Badge bg="info" className="bg-opacity-25 text-body">
                          {project.documentUuids?.length ?? 0}
                        </Badge>
                      </td>
                      <td className="small text-muted text-nowrap">
                        {formatDate(project.createdAt)}
                      </td>
                      <td className="text-end">
                        <Dropdown align="end">
                          <Dropdown.Toggle
                            variant="link"
                            className="text-decoration-none p-0"
                            id={`dropdown-${project.uuid}`}
                          >
                            <FontAwesomeIcon icon={faEllipsisV} />
                          </Dropdown.Toggle>
                          <Dropdown.Menu>
                            <Dropdown.Item
                              onClick={() => setDocsProjectUuid(project.uuid)}
                            >
                              <FontAwesomeIcon
                                icon={faFileAlt}
                                className="me-2"
                              />
                              {t('aiAgents.table.actionDocuments')}
                            </Dropdown.Item>
                            <Dropdown.Item
                              onClick={() => setSettingsProject(project)}
                            >
                              <FontAwesomeIcon
                                icon={faSlidersH}
                                className="me-2"
                              />
                              {t('aiAgents.table.actionSettings')}
                            </Dropdown.Item>
                            <Dropdown.Item onClick={() => openEdit(project)}>
                              <FontAwesomeIcon icon={faEdit} className="me-2" />
                              {t('aiAgents.table.actionEdit')}
                            </Dropdown.Item>
                            <Dropdown.Divider />
                            <Dropdown.Item
                              className="text-danger"
                              onClick={() => setDeletingProject(project)}
                            >
                              <FontAwesomeIcon
                                icon={faTrash}
                                className="me-2"
                              />
                              {t('aiAgents.table.actionDelete')}
                            </Dropdown.Item>
                          </Dropdown.Menu>
                        </Dropdown>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
          )}
        </Card.Body>
      </Card>

      <ProjectFormModal
        show={showFormModal}
        onHide={closeFormModal}
        editingProject={editingProject}
      />

      <DeleteProjectModal
        show={!!deletingProject}
        onHide={() => setDeletingProject(null)}
        project={deletingProject}
      />

      <ManageDocumentsModal
        show={!!docsProjectUuid}
        onHide={() => setDocsProjectUuid(null)}
        project={projects.find(p => p.uuid === docsProjectUuid) ?? null}
      />

      <SettingsPanel
        show={!!settingsProject}
        onHide={() => setSettingsProject(null)}
        project={settingsProject}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

const AgentProjectsPage = () => (
  <>
    <Row className="g-3 mb-3">
      <Col xxl={12}>
        <AgentProjectsGreetings />
      </Col>
    </Row>
    <Row className="g-3">
      <Col xxl={12}>
        <ProjectsTable />
      </Col>
    </Row>
  </>
);

export default AgentProjectsPage;
