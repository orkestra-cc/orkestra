import { Suspense, useCallback, useId, useLayoutEffect, useRef } from 'react';
import OrkestraLoader from 'components/common/OrkestraLoader';
import type {
  ModuleAdminSection,
  ModuleAdminSectionProps
} from 'modules/types';

interface AdminSectionHostProps extends Omit<
  ModuleAdminSectionProps,
  'onBusyChange'
> {
  section: ModuleAdminSection;
  /** Records whether the section identified by `id` has a write in flight. */
  onSectionBusy: (id: string, busy: boolean) => void;
}

/**
 * Mounts one addon section and owns its entry in the page's busy set, for
 * exactly as long as the section is on screen.
 *
 * Each mounted section reports under its own id, so one section settling
 * never unlocks the page while another is still writing. A report that
 * arrives after the section left (a request settling, or starting, after
 * "Leave anyway") is ignored, and the entry is released on unmount — an
 * unmounted section can no longer say it settled. The release lives here,
 * not in an effect of the page: React runs a child's effects before its
 * parent's, so a page-level reset on section change would clear the lock a
 * section reports from its own mount effect.
 */
const AdminSectionHost = ({
  section,
  onSectionBusy,
  ...props
}: AdminSectionHostProps) => {
  const id = useId();
  const mounted = useRef(true);

  // A layout effect: it re-arms before any passive effect of the section
  // runs, including when StrictMode remounts the tree in development.
  useLayoutEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      onSectionBusy(id, false);
    };
  }, [id, onSectionBusy]);

  const onBusyChange = useCallback(
    (busy: boolean) => {
      if (mounted.current) onSectionBusy(id, busy);
    },
    [id, onSectionBusy]
  );

  const Section = section.component;
  return (
    <Suspense fallback={<OrkestraLoader />}>
      <Section {...props} onBusyChange={onBusyChange} />
    </Suspense>
  );
};

export default AdminSectionHost;
