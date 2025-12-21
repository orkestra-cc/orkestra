import { useEffect, useRef, useState } from 'react';
import KanbanColumnHeader from './KanbanColumnHeader';
import TaskCard from './TaskCard';
import AddAnotherForm from './AddAnotherForm';
import IconButton from 'components/common/IconButton';
import classNames from 'classnames';
import { useKanbanContext } from 'providers/KanbanProvider';
import { v4 as uuid } from 'uuid';
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy
} from '@dnd-kit/sortable';
import { UniqueIdentifier } from '@dnd-kit/core';
import type { KanbanItem } from 'reducers/kanbanReducer';

interface KanbanColumnProps {
  kanbanColumnItem: KanbanItem;
  overId: UniqueIdentifier | null;
}

const KanbanColumn = ({ kanbanColumnItem, overId }: KanbanColumnProps) => {
  const { addTaskCard, cardHeight } = useKanbanContext();
  const { id, name, items } = kanbanColumnItem;
  const [showForm, setShowForm] = useState(false);
  const formViewRef = useRef<HTMLDivElement>(null);

  const { setNodeRef, listeners, attributes, isDragging } = useSortable({
    id: kanbanColumnItem.id,
    data: {
      type: 'column'
    },
    disabled: showForm,
    transition: {
      duration: 1000,
      easing: 'linear'
    }
  });

  const handleSubmit = (cardData: { title?: string }) => {
    const randomNumber = parseInt(uuid().replace(/-/g, '').slice(0, 12), 16);
    const newCard = {
      id: randomNumber.toString(),
      title: cardData.title || ''
    };
    const isEmpty = !Object.keys(cardData).length;

    if (!isEmpty) {
      addTaskCard(id.toString(), newCard);
      setShowForm(false);
    }
  };

  useEffect(() => {
    const timeout = setTimeout(() => {
      if (formViewRef.current) {
        formViewRef.current.scrollIntoView({ behavior: 'smooth' });
      }
    }, 500);

    return () => clearTimeout(timeout);
  }, [showForm]);

  return (
    <div className={classNames('kanban-column', { 'form-added': showForm })}>
      <KanbanColumnHeader id={id} title={name} itemCount={items.length} />
      <div>
        <div
          id={`container-${id}`}
          className="kanban-items-container scrollbar"
          ref={setNodeRef}
          {...attributes}
          {...listeners}
          onClick={e => e.stopPropagation()}
        >
          <SortableContext
            id={id.toString()}
            items={items.map(item => item.id)}
            strategy={verticalListSortingStrategy}
          >
            {items.map(task => (
              <TaskCard
                key={task.id}
                task={task}
                columnId={kanbanColumnItem.id.toString()}
              />
            ))}
          </SortableContext>
          {isDragging && overId === kanbanColumnItem.id && (
            <div
              className="bg-200 rounded-3"
              style={{
                minHeight: `${cardHeight}px`,
                width: '100%',
                transition: 'height 0.2s ease'
              }}
            />
          )}
          {
            <AddAnotherForm
              onSubmit={handleSubmit}
              type="card"
              showForm={showForm}
              setShowForm={setShowForm}
            />
          }
          <div ref={formViewRef}></div>
        </div>
        {!showForm && (
          <div className="kanban-column-footer">
            <IconButton
              size="sm"
              variant="link"
              className="d-block w-100 btn-add-card text-decoration-none text-600"
              icon="plus"
              iconClassName="me-2"
              onClick={() => setShowForm(true)}
            >
              Add another card
            </IconButton>
          </div>
        )}
      </div>
    </div>
  );
};

export default KanbanColumn;
