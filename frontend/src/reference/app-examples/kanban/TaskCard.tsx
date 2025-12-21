import { useRef } from 'react';
import { Card, Dropdown, OverlayTrigger, Tooltip } from 'react-bootstrap';
import { useSortable } from '@dnd-kit/sortable';
import Background from 'components/common/Background';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import Avatar, { AvatarGroup } from 'components/common/Avatar';
import { useKanbanContext } from 'providers/KanbanProvider';
import { useAppContext } from 'providers/AppProvider';
import { CSS } from '@dnd-kit/utilities';
import createMarkup from 'helpers/createMarkup';

// Type definitions for Kanban features
interface KanbanLabel {
  text: string;
  type: string;
  id?: string;
}

interface KanbanMember {
  id?: string;
  name: string;
  url?: string;
  img?: string;
  email?: string;
}

interface KanbanAttachment {
  id: string;
  type: 'image' | 'document' | 'video';
  url: string;
  name?: string;
  className?: string;
}

interface KanbanChecklist {
  completed: number;
  totalCount: number;
  items?: KanbanChecklistItem[];
}

interface KanbanChecklistItem {
  id: string;
  text: string;
  completed: boolean;
}

interface KanbanTask {
  id: string | number;
  title?: string;
  description?: string;
  labels?: KanbanLabel[];
  members?: KanbanMember[];
  attachments?: KanbanAttachment[];
  checklist?: KanbanChecklist;
  dueDate?: string;
  priority?: 'low' | 'medium' | 'high';
  [key: string]: unknown;
}

interface TaskDropMenuProps {
  id: string | number;
}

const TaskDropMenu = ({ id }: TaskDropMenuProps) => {
  const removeTaskCard = useKanbanContext().removeTaskCard;

  const {
    config: { isRTL }
  } = useAppContext();

  const handleRemoveTaskCard = () => {
    removeTaskCard(id.toString());
  };

  return (
    <Dropdown
      onClick={e => e.stopPropagation()}
      align="end"
      className="font-sans-serif"
    >
      <Dropdown.Toggle
        variant="falcon-default"
        size="sm"
        className="kanban-item-dropdown-btn hover-actions dropdown-caret-none"
      >
        <FontAwesomeIcon icon="ellipsis-h" transform="shrink-2" />
      </Dropdown.Toggle>

      <Dropdown.Menu className="py-0" align={isRTL ? 'start' : 'end'}>
        <Dropdown.Item href="#!">Add Card</Dropdown.Item>
        <Dropdown.Item href="#!">Edit</Dropdown.Item>
        <Dropdown.Item href="#!">Copy link</Dropdown.Item>
        <Dropdown.Divider />
        <Dropdown.Item onClick={handleRemoveTaskCard} className="text-danger">
          Remove
        </Dropdown.Item>
      </Dropdown.Menu>
    </Dropdown>
  );
};

interface TaskCardProps {
  task: KanbanTask;
  columnId: string;
  cursor?: boolean;
  rotate?: boolean;
}

const TaskCard = ({ task, columnId, cursor, rotate }: TaskCardProps) => {
  const openKanbanModal = useKanbanContext().openKanbanModal;
  const currentUser = useKanbanContext().currentUser || { name: '' };
  const setCardHeight = useKanbanContext().setCardHeight;
  const cardHeight = useKanbanContext().cardHeight || 0;
  const cardRef = useRef<HTMLDivElement>(null);
  const image =
    task.attachments && task.attachments.find(item => item.type === 'image');

  const handleModalOpen = () => {
    openKanbanModal(image?.url || '');
  };

  const { active, setNodeRef, listeners, isDragging, transform, transition } =
    useSortable({
      id: task.id,
      data: {
        type: 'task',
        ...task,
        columnId
      }
    });

  const isDraggingTaskItem = active && active.data.current?.type === 'task';

  const handleMouseDownCapture = () => {
    if (cardRef.current === null) return;
    setCardHeight(cardRef.current.offsetHeight);
  };
  // styles we need to apply on draggables
  const styles = {
    transform: rotate ? 'rotate(-2deg)' : CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 2 : 'auto',
    cursor: cursor ? 'grabbing' : 'pointer'
  };

  return (
    <div className="kanban-item" ref={setNodeRef} style={styles} {...listeners}>
      {isDragging && isDraggingTaskItem ? (
        <div
          className="bg-200 rounded-3"
          style={{
            height: `${cardHeight}px`,
            width: '100%'
          }}
        />
      ) : (
        <Card
          className="kanban-item-card hover-actions-trigger"
          ref={cardRef}
          onClick={handleModalOpen}
          style={{
            opacity: isDragging ? 0 : 1
          }}
          onMouseDownCapture={handleMouseDownCapture}
        >
          {image && (
            <div
              className={`position-relative rounded-top-lg overflow-hidden ${image.className}`}
            >
              <Background image={image.url} />
            </div>
          )}
          <Card.Body>
            <div className="position-relative">
              <TaskDropMenu id={task.id} />
            </div>
            {task.labels && (
              <div className="mb-2">
                {task.labels.map(label => (
                  <SubtleBadge
                    key={label.text}
                    bg={label.type as BadgeColor}
                    className="py-1 me-1 mb-1"
                  >
                    {label.text}
                  </SubtleBadge>
                ))}
              </div>
            )}
            <p
              className="mb-0 fw-medium font-sans-serif stretched-link"
              dangerouslySetInnerHTML={createMarkup(task.title || '')}
            />
            {(task.members || task.attachments || task.checklist) && (
              <div className="kanban-item-footer cursor-default">
                <div className="text-500 z-index-2">
                  {task.members &&
                    task.members.find(
                      member => member.name === currentUser.name
                    ) && (
                      <span className="me-2">
                        <OverlayTrigger
                          placement="top"
                          overlay={
                            <Tooltip style={{ position: 'fixed' }}>
                              You're assigned in this card
                            </Tooltip>
                          }
                        >
                          <span>
                            <FontAwesomeIcon icon="eye" />
                          </span>
                        </OverlayTrigger>
                      </span>
                    )}
                  {task.attachments && (
                    <span className="me-2">
                      <OverlayTrigger
                        placement="top"
                        overlay={
                          <Tooltip style={{ position: 'fixed' }}>
                            Attachments
                          </Tooltip>
                        }
                      >
                        <span>
                          <FontAwesomeIcon icon="paperclip" className="me-1" />
                        </span>
                      </OverlayTrigger>
                      <span>{task.attachments.length}</span>
                    </span>
                  )}
                  {task.checklist && (
                    <span className="me-2">
                      <OverlayTrigger
                        placement="top"
                        overlay={
                          <Tooltip style={{ position: 'fixed' }}>
                            Attachments
                          </Tooltip>
                        }
                      >
                        <span>
                          <FontAwesomeIcon icon="check" className="me-1" />
                        </span>
                      </OverlayTrigger>
                      <span>{`${task.checklist.completed}/${task.checklist.totalCount}`}</span>
                    </span>
                  )}
                </div>
                <div className="z-index-2">
                  {task.members && (
                    <AvatarGroup>
                      {task.members.map(member => (
                        <OverlayTrigger
                          key={member.name}
                          placement="top"
                          overlay={
                            <Tooltip style={{ position: 'fixed' }}>
                              {member.name}
                            </Tooltip>
                          }
                        >
                          <div>
                            <Avatar
                              size="l"
                              src={member.url || member.img}
                              className="ms-n1"
                            />
                          </div>
                        </OverlayTrigger>
                      ))}
                    </AvatarGroup>
                  )}
                </div>
              </div>
            )}
          </Card.Body>
        </Card>
      )}
    </div>
  );
};

export default TaskCard;
