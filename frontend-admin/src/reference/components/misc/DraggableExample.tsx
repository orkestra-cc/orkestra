import { useState } from 'react';
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import { Link } from 'react-router';
import { DndContext, closestCorners, DragOverlay } from '@dnd-kit/core';
import paths from 'routes/paths';
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useGetDndSensor } from 'hooks/ui/useGetDndSensor';

const data = [
  {
    id: 1,
    contents: [
      {
        id: 10000001,
        text: 'Add a pdf file that describes all the symptoms of COVID-19'
      },
      {
        id: 10000002,
        text: 'Make a Registration form that include Name, Email and a Password input field'
      },
      {
        id: 10000003,
        text: 'Add a cookie notice which will be shown in the bottom of the page and have a link of "Privacy Policy"'
      },
      {
        id: 10000004,
        text: 'Update profile page layout with cover image and user setting menu'
      }
    ]
  },
  {
    id: 2,
    contents: [
      {
        id: 20000001,
        text: 'Update all the npm packages and also remove the outdated plugins'
      },
      {
        id: 20000002,
        text: 'Add a getting started page that allows users to see the starting process'
      },
      {
        id: 20000003,
        text: 'Review and test all the task and deploy to the server'
      },
      {
        id: 20000004,
        text: 'Get all the data by API call and show them to the landing page by adding a new section'
      }
    ]
  }
];

const draggableCode = `DraggableComponent = () => {
  const [draggableData, setDraggableData] = useState(data);
  const [activeTask, setActiveTask] = useState(null);
  const [columnId, setColumnId] = useState(null);

  const sensor = useGetDndSensor();


  const findColumn = (id) => {
    return draggableData.find(
      (col) => col.contents.some((item) => item.id === id) || col.id === id
    );
  };

  const handleDragStart = event  => {
    setActiveTask(event.active.data.current?.item);
    setColumnId(event.active.data.current?.columnId);
  }
  const handleDragOver = ({ active, over }) => {
    if (!active || !over) return;

    const activeId = active.id;
    const overId = over.id;

    const activeColumn = findColumn(activeId);
    const overColumn = findColumn(overId);

    if (!activeColumn || !overColumn || activeColumn.id === overColumn.id) return;

    setDraggableData((prevData) => {
      const activeItemIndex = activeColumn.contents.findIndex(
        (item) => item.id === activeId
      );

      if (activeItemIndex === -1) return prevData;

      const activeItem = activeColumn.contents[activeItemIndex];
      const updatedActiveContents = activeColumn.contents.filter(
        (item) => item.id !== activeId
      );

      const updatedOverContents = [...overColumn.contents];
      const overItemIndex = overColumn.contents.findIndex(
        (item) => item.id === overId
      );

      updatedOverContents.splice(
        overItemIndex >= 0 ? overItemIndex : updatedOverContents.length,
        0,
        activeItem
      );

      return prevData.map((column) => {
        if (column.id === activeColumn.id) {
          return { ...column, contents: updatedActiveContents };
        }
        if (column.id === overColumn.id) {
          return { ...column, contents: updatedOverContents };
        }
        return column;
      });
    });
  };
  const handleDragEnd = ({ active, over }) => {
    if (!over) return;
    const activeColumnId = active.data.current.columnId;
    const overColumnId = over.data.current.columnId || over.id;
    if (activeColumnId === overColumnId) {
      const columnIndex = draggableData.findIndex(
        (col) => col.id === activeColumnId
      );
      const column = draggableData[columnIndex];
      const oldIndex = column.contents.findIndex(
        (item) => item.id === active.id
      );
      const newIndex = column.contents.findIndex((item) => item.id === over.id);

      const updatedContents = arrayMove(column.contents, oldIndex, newIndex);

      const newDraggableData = [...draggableData];
      newDraggableData[columnIndex] = { ...column, contents: updatedContents };
      setDraggableData(newDraggableData);
    } else {
      const sourceColumnIndex = draggableData.findIndex(
        (col) => col.id === activeColumnId
      );
      const destinationColumnIndex = draggableData.findIndex(
        (col) => col.id === overColumnId
      );

      const activeTask = draggableData[sourceColumnIndex].contents.find(
        (item) => item.id === active.id
      );

      const newDraggableData = [...draggableData];
      newDraggableData[sourceColumnIndex].contents = newDraggableData[
        sourceColumnIndex
      ].contents.filter((item) => item.id !== active.id);
      newDraggableData[destinationColumnIndex].contents = [
        ...newDraggableData[destinationColumnIndex].contents,
        activeTask
      ];

      setDraggableData(newDraggableData);
    }
    setActiveTask(null);
    setColumnId(null);
  };

  return (
    <DndContext 
      sensors={sensor} 
      collisionDetection={closestCorners} 
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <Row>
        {
          draggableData.map(column => (
            <Col lg={6} key={column.id}>
              <div className="kanban-items-container border bg-white dark__bg-1000 rounded-2 py-3 mb-3">
                <SortableContext 
                  items={column.contents.map(item => item.id)} 
                  strategy={verticalListSortingStrategy}
                >
                  {
                    column.contents.map(content => 
                      <SortableItem 
                        columnId={column.id}
                        id={content.id}
                        key={content.id}
                        item={content}
                      >
                        <Card className="mb-3 kanban-item shadow-sm dark__bg-1100">
                          <Card.Body>
                            <p className="fs-10 fw-medium font-sans-serif mb-0">
                              {content.text}
                            </p>
                          </Card.Body>
                        </Card>
                      </SortableItem>
                    )
                  }
                </SortableContext>
              </div>
            </Col>  
          ))
        }
      </Row>
      <DragOverlay>
        {
          activeTask && 
             <Card className="mb-3 kanban-item shadow-sm dark__bg-1100">
                <Card.Body>
                  <p className="fs-10 fw-medium font-sans-serif mb-0">
                    {activeTask.text}
                  </p>
                </Card.Body>
              </Card>
        }
      </DragOverlay>
    </DndContext>
  );
};`;

interface SortableItemProps {
  item: { id: number; text: string };
  columnId: number;
  id: number;
  children: React.ReactNode;
}

const SortableItem = ({ item, columnId, id, children }: SortableItemProps) => {
  const {
    setNodeRef,
    attributes,
    listeners,
    transform,
    transition,
    isDragging
  } = useSortable({
    id,
    data: {
      type: 'task',
      item: item,
      columnId
    }
  });

  const styles = {
    transform: CSS.Transform.toString(transform),
    transition,
    cursor: isDragging ? 'grab' : 'pointer',
    zIndex: isDragging ? 2 : 'auto',
    opacity: isDragging ? 0.8 : 1
  };
  return (
    <div ref={setNodeRef} {...attributes} {...listeners} style={styles}>
      {children}
    </div>
  );
};

const DraggableExample = () => (
  <>
    <PageHeader
      title="Draggable"
      description={`Beautiful and accessible drag and drop for lists with React`}
      className="mb-3"
    >
      <Button
        href={`https://dndkit.com/`}
        target="_blank" rel="noopener noreferrer"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Draggable Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="Example">
        <p className="mt-2 mb-0">
          Here is the example of the Multiple Container Sortable feature of the
          Draggable library. We also design{' '}
          <Link to={paths.kanban}>Kanban Board</Link> using this Draggable
          Library.{' '}
          <b>You can drag any card in between the two columns below:</b>
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={draggableCode}
        scope={{
          useState,
          data,
          DndContext,
          closestCorners,
          DragOverlay,
          SortableContext,
          useSortable,
          arrayMove,
          verticalListSortingStrategy,
          SortableItem,
          useGetDndSensor
        }}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default DraggableExample;
