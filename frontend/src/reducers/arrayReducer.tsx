
import orderBy from 'lodash/orderBy';
import { toast } from 'react-toastify';

export interface ArrayItem {
  id: string | number;
  [key: string]: any;
}

export interface ArrayAction {
  type: 'ADD' | 'REMOVE' | 'EDIT' | 'SORT';
  id?: string | number;
  payload?: ArrayItem;
  sortBy?: string;
  order?: 'asc' | 'desc';
  isAddToStart?: boolean;
  isUpdatedStart?: boolean;
}

export const arrayReducer = (state: ArrayItem[], action: ArrayAction): ArrayItem[] => {
  const { type, id, payload, sortBy, order, isAddToStart, isUpdatedStart } =
    action;
  switch (type) {
    case 'ADD':
      if (!payload) {
        return state;
      }
      if (state.find(item => item.id === payload.id)) {
        toast(
          <span className="text-warning">Item already exists in the list!</span>
        );
        return state;
      }
      if (isAddToStart) {
        return [payload, ...state];
      }
      return [...state, payload];
    case 'REMOVE':
      if (id !== 0 && !id) {
        return state;
      }
      return state.filter(item => item.id !== id);
    case 'EDIT':
      if ((id !== 0 && !id) || !payload) {
        return state;
      }
      if (isUpdatedStart) {
        const filteredState = state.filter(item => item.id !== id);
        return [payload, ...filteredState];
      }
      return state.map(item => (item.id === id ? payload : item));
    case 'SORT':
      if (!sortBy || !order) {
        return state;
      }
      return orderBy(state, sortBy, order);
    default:
      return state;
  }
};
