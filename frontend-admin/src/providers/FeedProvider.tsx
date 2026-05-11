import React, { createContext, useContext, useReducer, ReactNode, Dispatch } from 'react';
import { feedReducer, Feed, FeedAction } from 'reducers/feedReducer';
import rawFeeds from 'data/feed';

interface FeedContextValue {
  feeds: Feed[];
  feedDispatch: Dispatch<FeedAction>;
}

interface FeedProviderProps {
  children: ReactNode;
}

export const FeedContext = createContext<FeedContextValue | undefined>(undefined);

const FeedProvider: React.FC<FeedProviderProps> = ({ children }) => {
  const [feeds, feedDispatch] = useReducer(feedReducer, rawFeeds as Feed[]);

  const value: FeedContextValue = {
    feeds,
    feedDispatch
  };

  return (
    <FeedContext.Provider value={value}>
      {children}
    </FeedContext.Provider>
  );
};

export const useFeedContext = (): FeedContextValue => {
  const context = useContext(FeedContext);
  if (!context) {
    throw new Error('useFeedContext must be used within FeedProvider');
  }
  return context;
};

export default FeedProvider;
