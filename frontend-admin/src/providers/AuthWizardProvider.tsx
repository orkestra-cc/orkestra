import React, { createContext, useContext, useState, ReactNode } from 'react';

interface AuthWizardUser {
  [key: string]: any;
}

interface AuthWizardContextValue {
  user: AuthWizardUser;
  setUser: (user: AuthWizardUser) => void;
  step: number;
  setStep: (step: number) => void;
}

interface AuthWizardProviderProps {
  children: ReactNode;
}

export const AuthWizardContext = createContext<AuthWizardContextValue | undefined>(undefined);

const AuthWizardProvider: React.FC<AuthWizardProviderProps> = ({ children }) => {
  const [user, setUser] = useState<AuthWizardUser>({});
  const [step, setStep] = useState<number>(1);

  const value: AuthWizardContextValue = { user, setUser, step, setStep };
  return (
    <AuthWizardContext.Provider value={value}>
      {children}
    </AuthWizardContext.Provider>
  );
};

export const useAuthWizardContext = (): AuthWizardContextValue => {
  const context = useContext(AuthWizardContext);
  if (!context) {
    throw new Error('useAuthWizardContext must be used within AuthWizardProvider');
  }
  return context;
};

export default AuthWizardProvider;
