import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { resolveErrorMessage, type ApiErrorBody } from 'helpers/errorMessage';
import { jobsApi } from 'store/api/jobsApi';
import {
  useClearJobOverrideMutation,
  useRunJobNowMutation,
  useUpdateJobMutation
} from 'store/api/jobsApi';
import { useAppDispatch } from 'store/hooks';
import type { Job } from 'types/jobs';

interface RtkErrorLike {
  status?: number | string;
  data?: ApiErrorBody;
}

export const errorBody = (e: unknown): ApiErrorBody | undefined =>
  (e as RtkErrorLike | undefined)?.data;

export const errorStatus = (e: unknown): number =>
  Number((e as RtkErrorLike | undefined)?.status);

export interface JobActions {
  /** system.jobs.manage: whether any mutating control is offered. */
  canManage: boolean;
  /** Key of the job a write is in flight for. */
  busyKey: string | null;
  toggle: (job: Job) => Promise<void>;
  runNow: (job: Job) => Promise<void>;
  reset: (job: Job) => Promise<void>;
  /**
   * Shared failure path: a 409/404 means the console's copy is stale, so the
   * list is refetched; the message is localized from the error code.
   */
  fail: (e: unknown) => void;
}

/**
 * The row actions of the jobs workspace. Every write carries the revision the
 * console read; a conflict toasts and refetches instead of retrying blindly.
 */
export const useJobActions = (): JobActions => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const { hasPermission } = useAuth();
  const canManage = hasPermission('system.jobs.manage');
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [updateJob] = useUpdateJobMutation();
  const [clearOverride] = useClearJobOverrideMutation();
  const [runJobNow] = useRunJobNowMutation();

  return useMemo<JobActions>(() => {
    const fail = (e: unknown) => {
      const message = resolveErrorMessage(
        errorBody(e),
        t('adminJobs.errors.generic')
      );
      const status = errorStatus(e);
      if (status === 409 || status === 404) {
        dispatch(jobsApi.util.invalidateTags([{ type: 'Jobs', id: 'LIST' }]));
        toast.warn(message);
      } else {
        toast.error(message);
      }
    };

    const guarded = async (key: string, op: () => Promise<void>) => {
      setBusyKey(key);
      try {
        await op();
      } catch (e) {
        fail(e);
      } finally {
        setBusyKey(null);
      }
    };

    return {
      canManage,
      busyKey,
      fail,
      toggle: job =>
        guarded(job.key, async () => {
          await updateJob({
            key: job.key,
            revision: job.revision,
            enabled: !job.enabled
          }).unwrap();
        }),
      runNow: job =>
        guarded(job.key, async () => {
          await runJobNow({ key: job.key }).unwrap();
          toast.success(
            t('adminJobs.toasts.runRequested', { title: job.title })
          );
        }),
      reset: job =>
        guarded(job.key, async () => {
          await clearOverride({
            key: job.key,
            revision: job.revision
          }).unwrap();
          toast.success(t('adminJobs.toasts.reset', { title: job.title }));
        })
    };
  }, [canManage, busyKey, dispatch, t, updateJob, runJobNow, clearOverride]);
};
