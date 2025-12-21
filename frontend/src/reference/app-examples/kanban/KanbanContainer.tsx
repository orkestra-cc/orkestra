import { useEffect, useRef, useState } from 'react';
import KanbanColumn from './KanbanColumn';
import AddAnotherForm from './AddAnotherForm';
import KanbanModal from './KanbanModal';
import IconButton from 'components/common/IconButton';
import Bowser from 'bowser';
import { useKanbanContext } from 'providers/KanbanProvider';
import { DndContext, closestCorners, DragOverlay, DragStartEvent, DragOverEvent, DragEndEvent, UniqueIdentifier } from '@dnd-kit/core';

import { useGetDndSensor } from 'hooks/ui/useGetDndSensor';

import { arrayMove } from '@dnd-kit/sortable';
import TaskCard from './TaskCard';
import type { KanbanItem, TaskCard as TaskCardType } from 'reducers/kanbanReducer';

interface ListData {
  title?: string;
  [key: string]: unknown;
}

const KanbanContainer = () => {
  const kanbanItems = useKanbanContext().kanbanItems || [];
  const addKanbanColumn = useKanbanContext().addKanbanColumn;
  const updateSingleColumn = useKanbanContext().updateSingleColumn;
  const updateDualColumn = useKanbanContext().updateDualColumn;

  const [showForm, setShowForm] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const [activeTask, setActiveTask] = useState<TaskCardType | null>(null);
  const [overId, setOverId] = useState<UniqueIdentifier | null>(null);

  const sensor = useGetDndSensor();

  const handleSubmit = (listData: ListData) => {
    const listId = Math.max(...kanbanItems.map(item => parseInt(item.id))) + 1;
    const newList = {
      id: listId.toString(),
      name: listData.title || '',
      items: []
    };
    const isEmpty = !Object.keys(listData).length;

    if (!isEmpty) {
      addKanbanColumn(newList);
      setShowForm(false);
    }
  };

  useEffect(() => {
    const browser = Bowser.getParser(window.navigator.userAgent);
    const result = browser.getResult();
    const platform = result.platform;
    const browserInfo = result.browser;

    if (platform?.type === 'tablet' && containerRef.current) {
      containerRef.current.classList.add('ipad');
    }

    if (platform?.type === 'mobile' && containerRef.current) {
      containerRef.current.classList.add('mobile');
      if (browserInfo?.name === 'Safari') {
        containerRef.current.classList.add('safari');
      }
      if (browserInfo?.name === 'Chrome') {
        containerRef.current.classList.add('chrome');
      }
    }
  }, []);

  const findColumn = (id: UniqueIdentifier) => {
    return kanbanItems.find(
      (col: KanbanItem) => col.items.some((item: TaskCardType) => item.id === id) || col.id === id
    );
  };

  const getColumnIndex = (items: TaskCardType[], id: UniqueIdentifier) => {
    return items.findIndex((item: TaskCardType) => item.id === id);
  };

  const handleDragStart = (event: DragStartEvent) => {
    setActiveTask(event.active.data.current as TaskCardType);
  };
  const handleDragOver = (event: DragOverEvent) => {
    const { active, over } = event;
    if (!active || !over) return;

    const activeId = active.id;
    const overId = over.id;

    setOverId(overId);

    const activeColumn = findColumn(activeId);
    const overColumn = findColumn(overId);

    if (!activeColumn || !overColumn) return;

    if (activeColumn.id !== overColumn.id) {
      const overItems = overColumn.items;
      const activeItems = activeColumn.items;

      const activeIndex = getColumnIndex(activeItems, activeId);
      const overIndex = getColumnIndex(overItems, overId);

      const newIndex = overIndex >= 0 ? overIndex + 1 : overItems.length;

      updateDualColumn(
        activeColumn.id.toString(),
        overColumn.id.toString(),
        activeItems.filter(item => item.id !== activeId),
        [
          ...overItems.slice(0, newIndex),
          activeItems[activeIndex],
          ...overItems.slice(newIndex)
        ]
      );
    }
  };
  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!active || !over) return;

    const activeColumnId = active.data.current?.columnId;
    const overColumnId = over.data.current?.columnId || over.id;

    if (!activeColumnId || !overColumnId) return;

    if (activeColumnId === overColumnId) {
      const column = kanbanItems.find((col: KanbanItem) => col.id === activeColumnId);
      if (!column) return;
      const oldIndex = column.items.findIndex((item: TaskCardType) => item.id === active.id);
      const newIndex = column.items.findIndex((item: TaskCardType) => item.id === over.id);

      if (oldIndex < 0 || newIndex < 0) return;

      const reorderedItems = arrayMove(column.items, oldIndex, newIndex);

      updateSingleColumn(column.id.toString(), reorderedItems);
    } else {
      const sourceColumn = kanbanItems.find((col: KanbanItem) => col.id === activeColumnId);
      const destColumn = kanbanItems.find((col: KanbanItem) => col.id === overColumnId);
      if (!sourceColumn || !destColumn) return;

      const activeTask = sourceColumn.items.find((item: TaskCardType) => item.id === active.id);
      if (!activeTask) return;

      const updatedSourceItems = sourceColumn.items.filter(
        (item: TaskCardType) => item.id !== active.id
      );
      const updatedDestItems = [...destColumn.items, activeTask];

      updateDualColumn(
        sourceColumn.id.toString(),
        destColumn.id.toString(),
        updatedSourceItems,
        updatedDestItems
      );
    }

    setActiveTask(null);
  };

  return (
    <DndContext
      sensors={sensor}
      collisionDetection={closestCorners}
      onDragOver={handleDragOver}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
    >
      <div className="kanban-container me-n3 scrollbar" ref={containerRef}>
        {kanbanItems.map(kanbanColumnItem => (
          <KanbanColumn
            key={kanbanColumnItem.id}
            kanbanColumnItem={kanbanColumnItem}
            overId={overId}
          />
        ))}
        <div className="kanban-column">
          <AddAnotherForm
            type="list"
            onSubmit={handleSubmit}
            showForm={showForm}
            setShowForm={setShowForm}
          />
          {!showForm && (
            <IconButton
              variant="secondary"
              className="d-block w-100 border-400 bg-400"
              icon="plus"
              iconClassName="me-1"
              onClick={() => setShowForm(true)}
            >
              Add another list
            </IconButton>
          )}
        </div>
        <KanbanModal />
      </div>
      <DragOverlay>
        {activeTask && (
          <TaskCard task={activeTask} columnId="" cursor={true} rotate={true} />
        )}
      </DragOverlay>
    </DndContext>
  );
};

export default KanbanContainer;
