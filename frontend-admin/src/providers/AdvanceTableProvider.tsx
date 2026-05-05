import React, { createContext, useContext, ReactNode } from 'react';

interface AdvanceTableContextValue {
  [key: string]: any;
}

interface AdvanceTableProviderProps {
  children: ReactNode;
  [key: string]: any;
}

export const AdvanceTableContext = createContext<AdvanceTableContextValue | undefined>(undefined);

const AdvanceTableProvider: React.FC<AdvanceTableProviderProps> = ({ children, ...rest }) => {
  return (
    <AdvanceTableContext.Provider value={{ ...rest }}>
      {children}
    </AdvanceTableContext.Provider>
  );
};

export const useAdvanceTableContext = (): AdvanceTableContextValue => {
  const context = useContext(AdvanceTableContext);
  if (!context) {
    throw new Error('useAdvanceTableContext must be used within AdvanceTableProvider');
  }
  return context;
};

export default AdvanceTableProvider;
