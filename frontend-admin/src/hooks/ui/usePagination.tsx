import { useEffect, useReducer } from 'react';
import { chunk } from 'helpers/utils';

interface PaginationState<T> {
  allItems: T[];
  data: T[];
  pageChunk: T[][];
  totalPage: number;
  totalItems: number;
  itemsPerPage: number;
  currentPage: number;
  canNextPage: boolean;
  canPreviousPage: boolean;
  from: number;
  to: number;
  paginationArray: number[];
}

interface PaginationAction<T> {
  type: 'INIT' | 'NEXT_PAGE' | 'PREVIOUS_PAGE' | 'GO_TO_PAGE';
  payload?: {
    items?: T[];
    itemsPerPage?: number;
    currentPage?: number;
    pageNo?: number;
  };
}

const usePagination = <T = any>(items: T[], itemsPerPage: number = 5, currentPage: number = 1) => {
  const setFrom = (itemsPerPage: number, pageNo: number) => itemsPerPage * (pageNo - 1) + 1;
  const setTo = (itemsPerPage: number, pageNo: number, pageSize: number) =>
    itemsPerPage * (pageNo - 1) + pageSize;

  const paginationReducer = (state: PaginationState<T>, action: PaginationAction<T>): PaginationState<T> => {
    const { type, payload } = action;
    switch (type) {
      case 'INIT': {
        const items = payload?.items || [];
        const itemsPerPage = payload?.itemsPerPage || 5;
        const currentPage = payload?.currentPage || 1;
        const totalPage = Math.ceil(items.length / itemsPerPage);
        const pageChunk = chunk(items, itemsPerPage) as T[][];
        const data = pageChunk[currentPage - 1] || [];
        return {
          ...state,
          pageChunk,
          data,
          totalPage,
          totalItems: items.length,
          itemsPerPage,
          canNextPage: totalPage > currentPage,
          canPreviousPage: currentPage > 1,
          currentPage,
          paginationArray: Array.from(Array(totalPage).keys()).map(
            item => item + 1
          ),
          from: setFrom(itemsPerPage, currentPage),
          to: setTo(itemsPerPage, currentPage, data.length)
        };
      }
      case 'NEXT_PAGE': {
        const data = state.pageChunk[state.currentPage]
          ? state.pageChunk[state.currentPage]
          : state.data;
        return {
          ...state,
          data,
          currentPage:
            state.currentPage < state.totalPage
              ? state.currentPage + 1
              : state.currentPage,
          canNextPage: state.totalPage > state.currentPage + 1,
          canPreviousPage: state.totalPage > 1,
          from: setFrom(state.itemsPerPage, state.currentPage + 1),
          to: setTo(state.itemsPerPage, state.currentPage + 1, data.length)
        };
      }
      case 'PREVIOUS_PAGE': {
        const data = state.pageChunk[state.currentPage - 2]
          ? state.pageChunk[state.currentPage - 2]
          : state.data;
        return {
          ...state,
          data,
          currentPage:
            state.currentPage > 1 ? state.currentPage - 1 : state.currentPage,
          canNextPage: state.totalPage > 1,
          canPreviousPage: state.currentPage - 1 > 1,
          from: setFrom(state.itemsPerPage, state.currentPage - 1),
          to: setTo(state.itemsPerPage, state.currentPage - 1, data.length)
        };
      }
      case 'GO_TO_PAGE': {
        const pageNo = payload?.pageNo || 1;
        const data = state.pageChunk[pageNo - 1] || [];
        return {
          ...state,
          data,
          currentPage: pageNo,
          canNextPage: state.totalPage > pageNo,
          canPreviousPage: pageNo > 1,
          from: setFrom(state.itemsPerPage, pageNo),
          to: setTo(state.itemsPerPage, pageNo, data.length)
        };
      }
      default:
        return state;
    }
  };

  const [paginationState, dispatch] = useReducer(paginationReducer, {
    allItems: [] as T[],
    data: [] as T[],
    pageChunk: [] as T[][],
    totalPage: 0,
    totalItems: 0,
    itemsPerPage: 0,
    currentPage: 1,
    canNextPage: false,
    canPreviousPage: false,
    from: 0,
    to: 0,
    paginationArray: []
  });

  const goToPage = (pageNo: number) => {
    dispatch({
      type: 'GO_TO_PAGE',
      payload: {
        pageNo
      }
    });
  };

  const nextPage = () => {
    dispatch({
      type: 'NEXT_PAGE'
    });
  };

  const prevPage = () => {
    dispatch({
      type: 'PREVIOUS_PAGE'
    });
  };

  const setItemsPerPage = (no: number) => {
    dispatch({
      type: 'INIT',
      payload: {
        items,
        itemsPerPage: no,
        currentPage
      }
    });
  };

  useEffect(() => {
    dispatch({
      type: 'INIT',
      payload: {
        items,
        itemsPerPage,
        currentPage
      }
    });
  }, [items]);

  return {
    paginationState,
    nextPage,
    prevPage,
    goToPage,
    setItemsPerPage
  };
};

export default usePagination;
