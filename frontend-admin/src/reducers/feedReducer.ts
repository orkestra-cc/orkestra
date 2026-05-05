export interface Feed {
  id: string | number;
  [key: string]: any;
}

export interface FeedAction {
  type: 'ADD' | 'UPDATE';
  payload?: {
    id: string | number;
    feed?: Feed;
    [key: string]: any;
  };
}

export const feedReducer = (state: Feed[], action: FeedAction): Feed[] => {
  const { type, payload } = action;
  switch (type) {
    case 'ADD':
      if (!payload) {
        return state;
      }
      return [payload, ...state];

    case 'UPDATE':
      return state.map(feed => (feed.id === payload?.id ? payload?.feed || feed : feed));
    default:
      return state;
  }
};
