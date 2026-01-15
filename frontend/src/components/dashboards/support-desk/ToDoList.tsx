import IconButton from 'components/common/IconButton';
import { useState } from 'react';
import { Button, Card, Form } from 'react-bootstrap';
import { todoList } from 'data/dashboard/support-desk';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';

interface ToDoItem {
  id: string | number;
  task: string;
  title: string;
  completed: boolean;
}

interface ToDoItemsProps {
  item: ToDoItem;
  index: number;
  length: number;
  handleChange: (id: string | number, completed: boolean) => void;
}

interface ToDoListProps {
  previewClassName?: string;
}

const ToDoItems = ({ item, index, length, handleChange }: ToDoItemsProps) => {
  return (
    <div
      className={classNames(
        'd-flex hover-actions-trigger btn-reveal-trigger gap-3 border-200',
        { 'border-bottom mb-3': index !== length - 1 }
      )}
    >
      <Form.Check className="mb-0">
        <Form.Check.Input
          id={`ticket-todo-${index}`}
          onChange={e => handleChange(item.id, e.target.checked)}
          className={classNames('mb-1', {
            'form-check-line-through': item.completed
          })}
        />
        <Form.Check.Label
          className="w-100 pe-3"
          htmlFor={`ticket-todo-${index}`}
        >
          <span className="mb-1 text-700 d-block">{item.task}</span>
          <span
            className={classNames(
              'fs-11 text-600 lh-base font-base fw-normal d-block',
              { 'mb-2': index !== length - 1 }
            )}
          >
            {item.title}
          </span>
        </Form.Check.Label>
      </Form.Check>
      <div className="hover-actions end-0">
        <Button variant="link" className="fs-11 icon-item-sm px-0 text-600">
          <FontAwesomeIcon icon="trash" className="text-danger" />
        </Button>
      </div>
    </div>
  );
};

const ToDoList = ({ previewClassName }: ToDoListProps) => {
  const [todoItems, setTodoItems] = useState<ToDoItem[]>(todoList as ToDoItem[]);
  const handleChange = (id: string | number, completed: boolean) => {
    const updatedTodos = todoItems.map((item: ToDoItem) =>
      item.id === id ? { ...item, completed } : item
    );
    setTodoItems(updatedTodos);
  };
  return (
    <Card className={classNames('h-100', previewClassName)}>
      <Card.Header className="d-flex flex-between-center bg-body-tertiary">
        <h6 className="mb-0">To-do List</h6>
        <IconButton
          variant="falcon-default"
          size="sm"
          icon="plus"
          transform="shrink-3"
        >
          <span>Add</span>
        </IconButton>
      </Card.Header>
      <Card.Body className="ticket-todo-list scrollbar">
        {todoItems.map((item, index) => (
          <ToDoItems
            item={item}
            index={index}
            length={todoItems.length}
            key={item.id}
            handleChange={handleChange}
          />
        ))}
      </Card.Body>
      <Card.Footer className="text-center border-top border-200 p-0">
        <a href="#!" className="btn btn-link btn-sm px-0 fw-medium py-2">
          View all
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </a>
      </Card.Footer>
    </Card>
  );
};

export default ToDoList;
