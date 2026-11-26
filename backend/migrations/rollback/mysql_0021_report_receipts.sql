-- Reverting the receipt store disables replay acknowledgements.
DROP TABLE IF EXISTS device_report_receipt;
DELETE FROM schema_migrations WHERE name='mysql/0021_report_receipts.sql';
