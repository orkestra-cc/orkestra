import { setupServer } from 'msw/node';
import { defaultHandlers } from './handlers';

// Single MSW server shared across the whole test run. Lifecycle is wired
// in src/test/setup.ts (listen / resetHandlers / close).
export const server = setupServer(...defaultHandlers);
