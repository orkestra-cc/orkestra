import { createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from '../index';
import currentUserAvatar from 'assets/img/team/3.jpg';
import {
  members,
  labels,
  attachments,
  kanbanItems,
  comments,
  activities
} from 'data/kanban';

interface Member {
  id: string;
  name: string;
  img: string;
  role: string;
}

interface Label {
  text: string;
  type: string;
}

interface Attachment {
  id: string;
  image?: string;
  src?: string;
  title: string;
  date: string;
  type: string;
}

interface Comment {
  id: string;
  author: string;
  content: string;
  date: string;
  avatar?: string;
}

interface Activity {
  id: string;
  user: string;
  action: string;
  timestamp: string;
  details?: string;
}

interface KanbanItem {
  id: string;
  name: string;
  items: TaskCard[];
}

interface TaskCard {
  id: string;
  title: string;
  description?: string;
  assignees?: Member[];
  labels?: Label[];
  attachments?: Attachment[];
  comments?: Comment[];
  dueDate?: string;
  priority?: 'low' | 'medium' | 'high';
  completed?: boolean;
}

interface CurrentUser {
  name: string;
  avatarSrc: string;
  profileLink: string;
  institutionLink: string;
}

interface KanbanModal {
  show: boolean;
  modalContent: {
    image?: string;
    [key: string]: any;
  };
}

interface KanbanState {
  members: Member[];
  labels: Label[];
  attachments: Attachment[];
  kanbanItems: KanbanItem[];
  comments: Comment[];
  activities: Activity[];
  kanbanModal: KanbanModal;
  cardHeight: number;
  currentUser: CurrentUser;
}

const initialState: KanbanState = {
  members: members,
  labels: labels,
  attachments: attachments,
  kanbanItems: kanbanItems,
  comments: comments,
  activities: activities,
  kanbanModal: {
    show: false,
    modalContent: {}
  },
  cardHeight: 0,
  currentUser: {
    name: 'Emma',
    avatarSrc: currentUserAvatar,
    profileLink: '/user/profile',
    institutionLink: '#!'
  }
};

const kanbanSlice = createSlice({
  name: 'kanban',
  initialState,
  reducers: {
    // Modal actions
    openKanbanModal: (state, action: PayloadAction<string>) => {
      state.kanbanModal.modalContent.image = action.payload;
      state.kanbanModal.show = true;
    },

    toggleKanbanModal: (state) => {
      state.kanbanModal.show = !state.kanbanModal.show;
    },

    closeKanbanModal: (state) => {
      state.kanbanModal.show = false;
      state.kanbanModal.modalContent = {};
    },

    // Column actions
    addKanbanColumn: (state, action: PayloadAction<KanbanItem>) => {
      state.kanbanItems.push(action.payload);
    },

    removeKanbanColumn: (state, action: PayloadAction<string>) => {
      state.kanbanItems = state.kanbanItems.filter(
        item => item.id !== action.payload
      );
    },

    updateKanbanColumn: (state, action: PayloadAction<{ id: string; updates: Partial<KanbanItem> }>) => {
      const { id, updates } = action.payload;
      const columnIndex = state.kanbanItems.findIndex(item => item.id === id);
      if (columnIndex !== -1) {
        state.kanbanItems[columnIndex] = { ...state.kanbanItems[columnIndex], ...updates };
      }
    },

    // Task card actions
    addTaskCard: (state, action: PayloadAction<{ targetListId: string; newCard: TaskCard }>) => {
      const { targetListId, newCard } = action.payload;
      const column = state.kanbanItems.find(item => item.id === targetListId);
      if (column) {
        column.items.push(newCard);
      }
    },

    removeTaskCard: (state, action: PayloadAction<string>) => {
      state.kanbanItems.forEach(column => {
        column.items = column.items.filter(item => item.id !== action.payload);
      });
    },

    updateTaskCard: (state, action: PayloadAction<{ cardId: string; updates: Partial<TaskCard> }>) => {
      const { cardId, updates } = action.payload;
      state.kanbanItems.forEach(column => {
        const cardIndex = column.items.findIndex(item => item.id === cardId);
        if (cardIndex !== -1) {
          column.items[cardIndex] = { ...column.items[cardIndex], ...updates };
        }
      });
    },

    // Drag and drop actions - optimized for performance
    updateSingleColumn: (state, action: PayloadAction<{ columnId: string; reorderedItems: TaskCard[] }>) => {
      const { columnId, reorderedItems } = action.payload;
      const column = state.kanbanItems.find(item => item.id === columnId);
      if (column) {
        column.items = [...reorderedItems];
      }
    },

    updateDualColumn: (state, action: PayloadAction<{
      sourceColumnId: string;
      destColumnId: string;
      updatedSourceItems: TaskCard[];
      updatedDestItems: TaskCard[];
    }>) => {
      const { sourceColumnId, destColumnId, updatedSourceItems, updatedDestItems } = action.payload;

      const sourceColumn = state.kanbanItems.find(item => item.id === sourceColumnId);
      const destColumn = state.kanbanItems.find(item => item.id === destColumnId);

      if (sourceColumn) {
        sourceColumn.items = updatedSourceItems;
      }
      if (destColumn) {
        destColumn.items = updatedDestItems;
      }
    },

    // UI state actions
    setCardHeight: (state, action: PayloadAction<number>) => {
      state.cardHeight = action.payload;
    },

    updateCurrentUser: (state, action: PayloadAction<Partial<CurrentUser>>) => {
      state.currentUser = { ...state.currentUser, ...action.payload };
    },

    // Data management actions
    addMember: (state, action: PayloadAction<Member>) => {
      state.members.push(action.payload);
    },

    removeMember: (state, action: PayloadAction<string>) => {
      state.members = state.members.filter(member => member.id !== action.payload);
    },

    addLabel: (state, action: PayloadAction<Label>) => {
      state.labels.push(action.payload);
    },

    removeLabel: (state, action: PayloadAction<string>) => {
      state.labels = state.labels.filter(label => label.text !== action.payload);
    },

    addAttachment: (state, action: PayloadAction<Attachment>) => {
      state.attachments.push(action.payload);
    },

    removeAttachment: (state, action: PayloadAction<string>) => {
      state.attachments = state.attachments.filter(attachment => attachment.id !== action.payload);
    },

    addComment: (state, action: PayloadAction<Comment>) => {
      state.comments.push(action.payload);
    },

    removeComment: (state, action: PayloadAction<string>) => {
      state.comments = state.comments.filter(comment => comment.id !== action.payload);
    },

    addActivity: (state, action: PayloadAction<Activity>) => {
      state.activities.unshift(action.payload);
    },

    // Utility actions
    resetKanbanState: (state) => {
      Object.assign(state, initialState);
    }
  }
});

export const {
  openKanbanModal,
  toggleKanbanModal,
  closeKanbanModal,
  addKanbanColumn,
  removeKanbanColumn,
  updateKanbanColumn,
  addTaskCard,
  removeTaskCard,
  updateTaskCard,
  updateSingleColumn,
  updateDualColumn,
  setCardHeight,
  updateCurrentUser,
  addMember,
  removeMember,
  addLabel,
  removeLabel,
  addAttachment,
  removeAttachment,
  addComment,
  removeComment,
  addActivity,
  resetKanbanState
} = kanbanSlice.actions;

// Selectors
export const selectKanban = (state: RootState) => state.kanban;
export const selectKanbanItems = (state: RootState) => state.kanban.kanbanItems;
export const selectKanbanModal = (state: RootState) => state.kanban.kanbanModal;
export const selectMembers = (state: RootState) => state.kanban.members;
export const selectLabels = (state: RootState) => state.kanban.labels;
export const selectAttachments = (state: RootState) => state.kanban.attachments;
export const selectComments = (state: RootState) => state.kanban.comments;
export const selectActivities = (state: RootState) => state.kanban.activities;
export const selectCardHeight = (state: RootState) => state.kanban.cardHeight;
export const selectCurrentUser = (state: RootState) => state.kanban.currentUser;

export const selectKanbanItemById = (id: string) => (state: RootState) => {
  return state.kanban.kanbanItems.find(item => item.id === id);
};

export const selectTaskById = (taskId: string) => (state: RootState) => {
  for (const column of state.kanban.kanbanItems) {
    const task = column.items.find(item => item.id === taskId);
    if (task) return task;
  }
  return null;
};

export const selectTasksByColumnId = (columnId: string) => (state: RootState) => {
  const column = state.kanban.kanbanItems.find(item => item.id === columnId);
  return column ? column.items : [];
};

export default kanbanSlice.reducer;