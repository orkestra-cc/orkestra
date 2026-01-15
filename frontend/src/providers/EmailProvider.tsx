import React, { createContext, useContext, useReducer, ReactNode, Dispatch } from 'react';
import { emailReducer, Email as EmailType, EmailState as EmailStateType } from 'reducers/emailReducer';
import rawEmails from 'data/email/emails';

// Type definitions
type EmailFilter = 'all' | 'unread' | 'star' | 'attachments' | 'archive' | 'snooze';

// Extended EmailState with filters array
interface EmailState extends EmailStateType {
  filters: EmailFilter[];
}

// Use the EmailAction type from the reducer
type EmailAction = import('reducers/emailReducer').EmailAction;

interface EmailContextValue {
  emailState: EmailState;
  emailDispatch: Dispatch<EmailAction>;
}

interface EmailProviderProps {
  children: ReactNode;
}

export const EmailContext = createContext<EmailContextValue | undefined>(undefined);

const EmailProvider: React.FC<EmailProviderProps> = ({ children }) => {
  const [emailState, emailDispatch] = useReducer(emailReducer, {
    emails: rawEmails as EmailType[],
    allEmails: rawEmails as EmailType[],
    currentFilter: 'all'
  });

  const extendedEmailState: EmailState = {
    ...emailState,
    filters: ['all', 'unread', 'star', 'attachments', 'archive', 'snooze'] as EmailFilter[]
  };

  const value: EmailContextValue = {
    emailState: extendedEmailState,
    emailDispatch
  };

  return (
    <EmailContext.Provider value={value}>
      {children}
    </EmailContext.Provider>
  );
};

export const useEmailContext = (): EmailContextValue => {
  const context = useContext(EmailContext);
  if (!context) {
    throw new Error('useEmailContext must be used within EmailProvider');
  }
  return context;
};

export default EmailProvider;
