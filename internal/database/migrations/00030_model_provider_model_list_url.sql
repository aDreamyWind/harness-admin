-- +goose Up
ALTER TABLE model_provider_connections
    ADD COLUMN model_list_url varchar(2048) NOT NULL DEFAULT '';
COMMENT ON COLUMN model_provider_connections.model_list_url IS '模型目录查询接口完整地址；为空时按供应商协议推导';

-- +goose Down
ALTER TABLE model_provider_connections DROP COLUMN model_list_url;
