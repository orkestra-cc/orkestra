// Persons tab of /marketing/contacts — built on the canonical
// useAdvanceTable + AdvanceTableProvider stack so it gets search,
// sort and pagination for free. Mirrors the "Searchable Table"
// reference at /reference/tables.

import { useMemo } from 'react';
import { Badge, Spinner } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import { formatDateForCSV } from 'utils/csvExport';

import {
  useListMarketingPersonsQuery,
  useListMarketingTagsQuery
} from 'store/api/marketingApi';
import type { Person, Tag } from 'types/marketing';

import ContactAvatar from './ContactAvatar';
import { fullName, primaryEmail } from './helpers';

const TagBadges = ({
  uuids,
  tagsByUUID
}: {
  uuids?: string[];
  tagsByUUID: Record<string, Tag>;
}) => {
  if (!uuids?.length) return <span className="text-400">—</span>;
  return (
    <>
      {uuids.map(uuid => {
        const tag = tagsByUUID[uuid];
        return (
          <Badge
            key={uuid}
            bg="info"
            pill
            className="me-1"
            style={tag?.color ? { backgroundColor: tag.color } : undefined}
          >
            {tag?.name ?? uuid.slice(0, 8)}
          </Badge>
        );
      })}
    </>
  );
};

const PersonsTable = () => {
  const { t } = useTranslation();
  const { data, isLoading } = useListMarketingPersonsQuery(undefined);
  const { data: tagsResp } = useListMarketingTagsQuery();

  const tagsByUUID = useMemo(() => {
    const map: Record<string, Tag> = {};
    tagsResp?.items?.forEach(tag => {
      map[tag.uuid] = tag;
    });
    return map;
  }, [tagsResp]);

  const columns = useMemo<ColumnDef<Person>[]>(
    () => [
      {
        id: 'name',
        // accessorFn (not accessorKey) so the global filter searches the
        // composed full name rather than missing firstName-only matches.
        accessorFn: row => fullName(row) || '—',
        header: t('marketing.contacts.list.colName'),
        cell: ({ row }) => {
          const p = row.original;
          const name = fullName(p);
          return (
            <div className="d-flex align-items-center gap-2">
              <ContactAvatar email={primaryEmail(p)} name={name} />
              <Link to={`/marketing/contacts/${p.uuid}`} className="fw-medium">
                {name || '—'}
              </Link>
            </div>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'email',
        accessorFn: row => primaryEmail(row),
        header: t('marketing.contacts.list.colEmail'),
        cell: ({ getValue }) =>
          (getValue() as string) || <span className="text-400">—</span>,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'tags',
        // Searching tag UUIDs is useless to operators; project to the
        // resolved tag names so the search box matches what they see.
        accessorFn: row =>
          (row.tags ?? [])
            .map(uuid => tagsByUUID[uuid]?.name ?? '')
            .filter(Boolean)
            .join(' '),
        header: t('marketing.contacts.list.colTags'),
        enableSorting: false,
        cell: ({ row }) => (
          <TagBadges uuids={row.original.tags} tagsByUUID={tagsByUUID} />
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'updatedAt',
        accessorKey: 'updatedAt',
        header: t('marketing.contacts.list.colUpdated'),
        cell: ({ getValue }) => (
          <small className="text-muted">
            {new Date(getValue() as string).toLocaleDateString()}
          </small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      }
    ],
    [t, tagsByUUID]
  );

  const persons = data?.items ?? [];
  const table = useAdvanceTable<Person>({
    data: persons,
    columns,
    sortable: true,
    pagination: true,
    perPage: 25
  });

  if (isLoading) {
    return (
      <div className="p-4 text-center text-muted">
        <Spinner animation="border" size="sm" className="me-2" />
        {t('marketing.contacts.list.loading')}
      </div>
    );
  }

  if (!persons.length) {
    return (
      <div className="p-4 text-center text-muted">
        {t('marketing.contacts.list.emptyPersons')}
      </div>
    );
  }

  return (
    <AdvanceTableProvider {...table}>
      <div className="d-flex flex-wrap justify-content-between align-items-center gap-2 px-x1 py-2 border-bottom border-200">
        <div className="flex-grow-1" style={{ maxWidth: 360 }}>
          <AdvanceTableSearchBox
            placeholder={t('marketing.contacts.list.searchPlaceholder')}
          />
        </div>
        <ExportCsvButton<Person>
          filename="marketing_persons"
          buildRow={p => ({
            UUID: p.uuid,
            FirstName: p.firstName ?? '',
            LastName: p.lastName ?? '',
            Email: primaryEmail(p),
            Tags: (p.tags ?? [])
              .map(uuid => tagsByUUID[uuid]?.name ?? uuid)
              .join('; '),
            Language: p.language ?? '',
            CreatedAt: formatDateForCSV(p.createdAt),
            UpdatedAt: formatDateForCSV(p.updatedAt)
          })}
        />
      </div>
      <AdvanceTable
        headerClassName="bg-body-tertiary align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          size: 'sm',
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="px-x1 py-2 border-top border-200">
        <AdvanceTableFooter
          rowInfo
          navButtons
          rowsPerPageSelection
          rowsPerPageOptions={[10, 25, 50, 100]}
        />
      </div>
    </AdvanceTableProvider>
  );
};

export default PersonsTable;
