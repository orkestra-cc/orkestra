import { useState } from 'react';
import { Card, Form } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import CardDropdown from 'components/common/CardDropdown';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import IconItem from 'components/common/icon/IconItem';
import classNames from 'classnames';
import { Link } from 'react-router';
import { todoList } from 'data/dashboard/projectManagement';
import FalconCardHeader from 'components/common/FalconCardHeader';

interface ToDoItem {
  id: string | number;
  task: string;
  completed: boolean;
  color: string;
}

interface ToDoListItemProps {
  item: ToDoItem;
  index: number;
  handleChange: (id: string | number, completed: boolean) => void;
}

const ToDoListItem = ({ item, index, handleChange }: ToDoListItemProps) => {
  return (
    <Flex
      justifyContent="between"
      className="hover-actions-trigger btn-reveal-trigger px-x1 hover-bg-100 border-200 border-top"
      alignItems="center"
    >
      <Form.Check
        className="py-2 my-2 d-flex align-items-center"
        id={`todoItem-${index}`}
      >
        <Form.Check.Input
          type="checkbox"
          defaultChecked={item.completed}
          onChange={e => handleChange(item.id, e.target.checked)}
          className={classNames(
            `mt-0 rounded-circle form-check-input-${item.color}`,
            {
              'form-check-line-through': item.completed
            }
          )}
        />
        <Form.Check.Label className="flex-1 mb-0 ms-3">
          {item.task}
        </Form.Check.Label>
      </Form.Check>
      <div className="d-flex align-items-center">
        <div className="hover-actions">
          <IconItem
            tag="button"
            icon="clock"
            size="sm"
            className="btn rounded-3 me-2 fs-11"
          />
          <IconItem
            tag="button"
            icon="user-plus"
            size="sm"
            className="btn rounded-3 me-2 fs-11"
          />
        </div>
        <CardDropdown
          btnRevealClass="btn-reveal-sm "
          drop={index > 1 ? 'start' : undefined}
        />
      </div>
    </Flex>
  );
};

const ToDoList = () => {
  const [todoItems, setTodoItems] = useState<ToDoItem[]>(todoList as ToDoItem[]);
  const handleChange = (id: string | number, completed: boolean) => {
    const updatedTodos = todoItems.map((item: ToDoItem) =>
      item.id === id ? { ...item, completed } : item
    );
    setTodoItems(updatedTodos);
  };
  return (
    <Card className="h-100">
      <FalconCardHeader title="To Do List" titleTag="h6" />
      <Card.Body className="p-0 overflow-hidden">
        {todoItems.map((item: ToDoItem, index: number) => (
          <ToDoListItem
            key={item.id}
            item={item}
            index={index}
            handleChange={handleChange}
          />
        ))}
      </Card.Body>
      <Card.Footer className="p-0 bg-body-tertiary">
        <Link
          to="#!"
          className="btn btn-link btn-sm w-100 py-2"
        >
          <FontAwesomeIcon icon="plus" className="me-1 fs-11" />
          Add New Task
        </Link>
      </Card.Footer>
    </Card>
  );
};

export default ToDoList;
