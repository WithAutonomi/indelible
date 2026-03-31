-- +goose Up

-- Store the serialized DataMap locally instead of on-network.
-- Used by the external signer flow where indelible controls the DataMap.
ALTER TABLE uploads ADD COLUMN data_map TEXT;

-- +goose Down
ALTER TABLE uploads DROP COLUMN data_map;
