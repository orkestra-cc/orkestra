import React, { createContext, useContext, ReactNode } from 'react';
import { useKanban } from 'hooks/redux/useKanban';

interface KanbanProviderProps {
  children: ReactNode;
  initialData?: any;
}

type KanbanStoreType = ReturnType<typeof useKanban>;

export const KanbanContext = createContext<KanbanStoreType | null>(null);

const KanbanProvider: React.FC<KanbanProviderProps> = ({ children, initialData: _initialData = null }) => {
  const kanbanStore = useKanban();

  return (
    <KanbanContext.Provider value={kanbanStore}>
      {children}
    </KanbanContext.Provider>
  );
};

export const useKanbanContext = (): KanbanStoreType => {
  const store = useContext(KanbanContext);
  if (!store) {
    throw new Error('useKanbanContext must be used within KanbanProvider');
  }
  return store;
};

// Alternative hook that uses the Redux hook directly (for components outside provider)
export { useKanban };

export default KanbanProvider;
