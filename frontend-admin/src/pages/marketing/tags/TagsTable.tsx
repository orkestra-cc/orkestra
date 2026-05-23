// Lightweight AdvanceTable wrapper for the Tags admin. The tags
// dataset is small per tenant, so search + sort is plenty — no
// pagination footer needed.

import { useMemo } from 'react';
import { Badge, Button } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { ColumnDef } from '@tanstack/react-table';

import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import IconButton from 'components/common/IconButton';
import { formatDateForCSV } from 'utils/csvExport';

import type { Tag } from 'types/marketing';

interface Props {
  tags: Tag[];
  onEdit: (tag: Tag) => void;
  onDelete: (tag: Tag) => void;
  onCreate: () => void;
}

const TagsTable = ({ tags, onEdit, onDelete, onCreate }: Props) => {
  const { t } = useTranslation();

  const columns = useMemo<ColumnDef<Tag>[]>(
    () => [
      {
        id: 'name',
        accessorKey: 'name',
        header: t('marketing.tags.colName'),
        cell: ({ getValue }) => (
          <span className="fw-medium">{String(getValue() ?? '')}</span>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'slug',
        accessorKey: 'slug',
        header: t('marketing.tags.colSlug'),
        cell: ({ getValue }) => (
          <code className="fs-10">{String(getValue() ?? '')}</code>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'path',
        accessorKey: 'path',
        header: t('marketing.tags.colPath'),
        cell: ({ getValue }) => (
          <small className="text-muted">{String(getValue() ?? '')}</small>
        ),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'color',
        accessorKey: 'color',
        header: t('marketing.tags.colColor'),
        enableSorting: false,
        cell: ({ getValue }) => {
          const color = getValue() as string | undefined;
          if (!color) return <span className="text-muted">—</span>;
          return (
            <Badge pill style={{ backgroundColor: color, color: '#fff' }}>
              {color}
            </Badge>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'actions',
        enableSorting: false,
        header: () => <span className="d-block text-end">&nbsp;</span>,
        cell: ({ row }) => (
          <div className="text-end">
            <Button
              size="sm"
              variant="link"
              onClick={() => onEdit(row.original)}
            >
              {t('marketing.tags.edit')}
            </Button>
            <Button
              size="sm"
              variant="link"
              className="text-danger"
              onClick={() => onDelete(row.original)}
            >
              {t('marketing.tags.delete')}
            </Button>
          </div>
        ),
        meta: { headerProps: { className: 'text-900 text-end' } }
      }
    ],
    [t, onEdit, onDelete]
  );

  const table = useAdvanceTable<Tag>({
    data: tags,
    columns,
    sortable: true,
    pagination: false
  });

  return (
    <AdvanceTableProvider {...table}>
      <div className="d-flex flex-wrap justify-content-between align-items-center gap-2 px-x1 py-2 border-bottom border-200">
        <div className="flex-grow-1" style={{ maxWidth: 360 }}>
          <AdvanceTableSearchBox
            placeholder={t('marketing.tags.searchPlaceholder')}
          />
        </div>
        <div className="d-flex align-items-center gap-2">
          <ExportCsvButton<Tag>
            filename="marketing_tags"
            buildRow={tag => ({
              UUID: tag.uuid,
              Name: tag.name,
              Slug: tag.slug,
              Path: tag.path,
              Color: tag.color ?? '',
              Description: tag.description ?? '',
              ParentUUID: tag.parentUuid ?? '',
              CreatedAt: formatDateForCSV(tag.createdAt),
              UpdatedAt: formatDateForCSV(tag.updatedAt)
            })}
          />
          <IconButton
            variant="orkestra-default"
            size="sm"
            icon="plus"
            transform="shrink-3"
            iconAlign="middle"
            onClick={onCreate}
          >
            <span className="ms-1">{t('marketing.tags.newTag')}</span>
          </IconButton>
        </div>
      </div>
      <AdvanceTable
        headerClassName="bg-body-tertiary align-middle"
        rowClassName="align-middle"
        tableProps={{
          size: 'sm',
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
    </AdvanceTableProvider>
  );
};

export default TagsTable;
