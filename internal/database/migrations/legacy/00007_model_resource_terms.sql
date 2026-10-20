-- +goose Up
COMMENT ON COLUMN model_provider_connections.code IS '企业内唯一的供应商连接编码，不区分大小写';
COMMENT ON COLUMN model_catalog_entries.code IS '企业内稳定且唯一的模型编码，不区分大小写';

-- +goose Down
COMMENT ON COLUMN model_provider_connections.code IS '企业内唯一的供应商连接代码，不区分大小写';
COMMENT ON COLUMN model_catalog_entries.code IS '企业内稳定且唯一的模型代码，不区分大小写';
