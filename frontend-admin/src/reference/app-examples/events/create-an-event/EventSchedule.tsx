import { useState } from 'react';
import { Card } from 'react-bootstrap';
import IconButton from 'components/common/IconButton';
import EventScheduleItem from './EventScheduleItem';
import { UseFormRegister, UseFormSetValue, FieldValues } from 'react-hook-form';

interface ScheduleItem {
  title: string;
  startDate: Date | null;
  startTime: Date | null;
  endDate: Date | null;
  endTime: Date | null;
}

interface EventScheduleProps {
  register: UseFormRegister<FieldValues>;
  setValue: UseFormSetValue<FieldValues>;
}

const EventSchedule = ({ register, setValue }: EventScheduleProps) => {
  const [items, setItems] = useState<ScheduleItem[]>([
    {
      title: '',
      startDate: null,
      startTime: null,
      endDate: null,
      endTime: null
    }
  ]);
  const handleAddSchedule = () => {
    setItems([
      ...items,
      {
        title: '',
        startDate: null,
        startTime: null,
        endDate: null,
        endTime: null
      }
    ]);
  };
  const handleRemoveItem = (index: number) => {
    setItems(items.filter((_item, ind) => index !== ind));
  };
  const handleChange = (id: number, name: string, value: string | Date | null) => {
    const newItems = [...items];
    const updatedItem = { ...newItems[id], [name]: value };
    setItems([
      ...newItems.slice(0, id),
      updatedItem,
      ...newItems.slice(id + 1)
    ]);
  };

  return (
    <Card className="mb-3">
      <Card.Header as="h5">Schedule</Card.Header>
      <Card.Body className="bg-body-tertiary">
        {items.map((item, index) => (
          <EventScheduleItem
            {...item}
            register={register}
            setValue={setValue}
            key={index}
            index={index}
            handleRemove={handleRemoveItem}
            handleChange={handleChange}
          />
        ))}

        <IconButton
          onClick={handleAddSchedule}
          variant="falcon-default"
          size="sm"
          icon="plus"
          transform="shrink-3"
        >
          Add Item
        </IconButton>
      </Card.Body>
    </Card>
  );
};

export default EventSchedule;
