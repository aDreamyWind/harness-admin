-- +goose Up
ALTER TABLE model_provider_connections
    ADD CONSTRAINT model_provider_connections_org_id_key UNIQUE (organization_id, id);

ALTER TABLE model_catalog_entries
    DROP CONSTRAINT IF EXISTS model_catalog_entries_provider_connection_id_fkey;

ALTER TABLE model_catalog_entries
    DROP CONSTRAINT IF EXISTS model_catalog_entries_provider_org_fk;

ALTER TABLE model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_provider_org_fk
    FOREIGN KEY (organization_id, provider_connection_id)
    REFERENCES model_provider_connections (organization_id, id);

-- +goose Down
ALTER TABLE model_catalog_entries
    DROP CONSTRAINT model_catalog_entries_provider_org_fk;

ALTER TABLE model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_provider_connection_id_fkey
    FOREIGN KEY (provider_connection_id)
    REFERENCES model_provider_connections (id);

ALTER TABLE model_provider_connections
    DROP CONSTRAINT model_provider_connections_org_id_key;
