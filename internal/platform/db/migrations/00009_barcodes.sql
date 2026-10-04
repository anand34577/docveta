-- +goose Up
-- Batch scanning: split a scan at separator sheets, and read archive-number labels.
ALTER TABLE spaces ADD COLUMN split_on_separators boolean NOT NULL DEFAULT false;
ALTER TABLE spaces ADD COLUMN read_asn_barcodes boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE spaces DROP COLUMN read_asn_barcodes;
ALTER TABLE spaces DROP COLUMN split_on_separators;
