/**
 * Utility functions for exporting data to CSV format
 */

/**
 * Escape CSV field value to handle special characters
 */
const escapeCsvValue = (value: any): string => {
  if (value === null || value === undefined) {
    return '';
  }

  const stringValue = String(value);

  // If the value contains comma, quote, or newline, wrap it in quotes
  if (stringValue.includes(',') || stringValue.includes('"') || stringValue.includes('\n')) {
    // Escape existing quotes by doubling them
    return `"${stringValue.replace(/"/g, '""')}"`;
  }

  return stringValue;
};

/**
 * Convert array of objects to CSV string
 */
export const arrayToCSV = (data: any[], headers: string[]): string => {
  if (!data || data.length === 0) {
    return headers.join(',');
  }

  // Create CSV header row
  const headerRow = headers.map(escapeCsvValue).join(',');

  // Create CSV data rows
  const dataRows = data.map(row => {
    return Object.values(row).map(escapeCsvValue).join(',');
  });

  return [headerRow, ...dataRows].join('\n');
};

/**
 * Download CSV file
 */
export const downloadCSV = (csv: string, filename: string): void => {
  // Add BOM for proper Excel UTF-8 support
  const BOM = '\uFEFF';
  const blob = new Blob([BOM + csv], { type: 'text/csv;charset=utf-8;' });
  const link = document.createElement('a');
  const url = URL.createObjectURL(blob);

  link.setAttribute('href', url);
  link.setAttribute('download', filename);
  link.style.visibility = 'hidden';

  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);

  // Clean up the URL object
  URL.revokeObjectURL(url);
};

/**
 * Format date to UK English locale
 */
export const formatDateForCSV = (dateString: string | undefined): string => {
  if (!dateString) return '';

  const date = new Date(dateString);
  return date.toLocaleString('en-GB', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  });
};

/**
 * Generate filename with timestamp
 */
export const generateTimestampedFilename = (baseName: string, extension: string = 'csv'): string => {
  const now = new Date();
  const timestamp = now.toISOString().split('T')[0]; // YYYY-MM-DD
  return `${baseName}_${timestamp}.${extension}`;
};