import { useCallback } from 'react';
import { useAppSelector, useAppDispatch } from '../../store/hooks';
import {
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
  resetKanbanState,
  selectKanban,
  selectKanbanItems,
  selectKanbanModal,
  selectMembers,
  selectLabels,
  selectAttachments,
  selectComments,
  selectActivities,
  selectCardHeight,
  selectCurrentUser,
  selectKanbanItemById,
  selectTaskById,
  selectTasksByColumnId
} from '../../store/slices/kanbanSlice';

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

export const useKanban = () => {
  const dispatch = useAppDispatch();
  const kanban = useAppSelector(selectKanban);
  const kanbanItems = useAppSelector(selectKanbanItems);
  const kanbanModal = useAppSelector(selectKanbanModal);
  const members = useAppSelector(selectMembers);
  const labels = useAppSelector(selectLabels);
  const attachments = useAppSelector(selectAttachments);
  const comments = useAppSelector(selectComments);
  const activities = useAppSelector(selectActivities);
  const cardHeight = useAppSelector(selectCardHeight);
  const currentUser = useAppSelector(selectCurrentUser);

  // Modal actions
  const openModal = useCallback((image: string) => {
    dispatch(openKanbanModal(image));
  }, [dispatch]);

  const toggleModal = useCallback(() => {
    dispatch(toggleKanbanModal());
  }, [dispatch]);

  const closeModal = useCallback(() => {
    dispatch(closeKanbanModal());
  }, [dispatch]);

  // Column actions
  const addColumn = useCallback((newColumn: KanbanItem) => {
    dispatch(addKanbanColumn(newColumn));
  }, [dispatch]);

  const removeColumn = useCallback((columnId: string) => {
    dispatch(removeKanbanColumn(columnId));
  }, [dispatch]);

  const updateColumn = useCallback((id: string, updates: Partial<KanbanItem>) => {
    dispatch(updateKanbanColumn({ id, updates }));
  }, [dispatch]);

  // Task card actions
  const addTask = useCallback((targetListId: string, newCard: TaskCard) => {
    dispatch(addTaskCard({ targetListId, newCard }));
  }, [dispatch]);

  const removeTask = useCallback((cardId: string) => {
    dispatch(removeTaskCard(cardId));
  }, [dispatch]);

  const updateTask = useCallback((cardId: string, updates: Partial<TaskCard>) => {
    dispatch(updateTaskCard({ cardId, updates }));
  }, [dispatch]);

  // Drag and drop actions
  const updateSingleCol = useCallback((columnId: string, reorderedItems: TaskCard[]) => {
    dispatch(updateSingleColumn({ columnId, reorderedItems }));
  }, [dispatch]);

  const updateDualCol = useCallback((
    sourceColumnId: string,
    destColumnId: string,
    updatedSourceItems: TaskCard[],
    updatedDestItems: TaskCard[]
  ) => {
    dispatch(updateDualColumn({
      sourceColumnId,
      destColumnId,
      updatedSourceItems,
      updatedDestItems
    }));
  }, [dispatch]);

  // UI state actions
  const updateCardHeight = useCallback((height: number) => {
    dispatch(setCardHeight(height));
  }, [dispatch]);

  const updateUser = useCallback((updates: Partial<CurrentUser>) => {
    dispatch(updateCurrentUser(updates));
  }, [dispatch]);

  // Data management actions
  const addNewMember = useCallback((member: Member) => {
    dispatch(addMember(member));
  }, [dispatch]);

  const removeMemberById = useCallback((memberId: string) => {
    dispatch(removeMember(memberId));
  }, [dispatch]);

  const addNewLabel = useCallback((label: Label) => {
    dispatch(addLabel(label));
  }, [dispatch]);

  const removeLabelByText = useCallback((labelText: string) => {
    dispatch(removeLabel(labelText));
  }, [dispatch]);

  const addNewAttachment = useCallback((attachment: Attachment) => {
    dispatch(addAttachment(attachment));
  }, [dispatch]);

  const removeAttachmentById = useCallback((attachmentId: string) => {
    dispatch(removeAttachment(attachmentId));
  }, [dispatch]);

  const addNewComment = useCallback((comment: Comment) => {
    dispatch(addComment(comment));
  }, [dispatch]);

  const removeCommentById = useCallback((commentId: string) => {
    dispatch(removeComment(commentId));
  }, [dispatch]);

  const addNewActivity = useCallback((activity: Activity) => {
    dispatch(addActivity(activity));
  }, [dispatch]);

  // Utility actions
  const resetKanban = useCallback(() => {
    dispatch(resetKanbanState());
  }, [dispatch]);

  // Selector functions
  const getKanbanItemById = useCallback((id: string) => {
    const selector = selectKanbanItemById(id);
    return selector(kanban as any);
  }, [kanban]);

  const getTaskById = useCallback((taskId: string) => {
    const selector = selectTaskById(taskId);
    return selector(kanban as any);
  }, [kanban]);

  const getTasksByColumnId = useCallback((columnId: string) => {
    const selector = selectTasksByColumnId(columnId);
    return selector(kanban as any);
  }, [kanban]);

  return {
    // State
    kanban,
    kanbanItems,
    kanbanModal,
    members,
    labels,
    attachments,
    comments,
    activities,
    cardHeight,
    currentUser,

    // Modal actions
    openKanbanModal: openModal,
    toggleKanbanModal: toggleModal,
    closeKanbanModal: closeModal,

    // Column actions
    addKanbanColumn: addColumn,
    removeKanbanColumn: removeColumn,
    updateKanbanColumn: updateColumn,

    // Task card actions
    addTaskCard: addTask,
    removeTaskCard: removeTask,
    updateTaskCard: updateTask,

    // Drag and drop actions
    updateSingleColumn: updateSingleCol,
    updateDualColumn: updateDualCol,

    // UI state actions
    setCardHeight: updateCardHeight,
    updateCurrentUser: updateUser,

    // Data management actions
    addMember: addNewMember,
    removeMember: removeMemberById,
    addLabel: addNewLabel,
    removeLabel: removeLabelByText,
    addAttachment: addNewAttachment,
    removeAttachment: removeAttachmentById,
    addComment: addNewComment,
    removeComment: removeCommentById,
    addActivity: addNewActivity,

    // Utility actions
    resetKanbanState: resetKanban,

    // Selector functions
    getKanbanItemById,
    getTaskById,
    getTasksByColumnId
  };
};

export const useKanbanItems = () => {
  return useAppSelector(selectKanbanItems);
};

export const useKanbanModal = () => {
  return useAppSelector(selectKanbanModal);
};

export const useKanbanMembers = () => {
  return useAppSelector(selectMembers);
};

export const useKanbanLabels = () => {
  return useAppSelector(selectLabels);
};

export const useKanbanAttachments = () => {
  return useAppSelector(selectAttachments);
};

export const useKanbanComments = () => {
  return useAppSelector(selectComments);
};

export const useKanbanActivities = () => {
  return useAppSelector(selectActivities);
};

export const useCardHeight = () => {
  return useAppSelector(selectCardHeight);
};

export const useCurrentUser = () => {
  return useAppSelector(selectCurrentUser);
};

export const useKanbanItemById = (id: string) => {
  return useAppSelector(selectKanbanItemById(id));
};

export const useTaskById = (taskId: string) => {
  return useAppSelector(selectTaskById(taskId));
};

export const useTasksByColumnId = (columnId: string) => {
  return useAppSelector(selectTasksByColumnId(columnId));
};