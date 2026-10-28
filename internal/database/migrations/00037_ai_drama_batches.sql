-- +goose Up
CREATE TABLE drama_batches (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
    batch_number integer NOT NULL CHECK (batch_number > 0),
    start_offset integer NOT NULL CHECK (start_offset >= 0),
    end_offset integer NOT NULL CHECK (end_offset > start_offset AND end_offset - start_offset <= 50000),
    selection_mode varchar(16) NOT NULL CHECK (selection_mode IN ('beginning', 'previous', 'manual')),
    previous_batch_id uuid REFERENCES drama_batches(id) ON DELETE SET NULL,
    boundary varchar(16) NOT NULL CHECK (boundary IN ('episode', 'chapter', 'paragraph', 'limit', 'end', 'manual')),
    status varchar(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'stale', 'ready')),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, batch_number)
);
CREATE INDEX drama_batches_project_number_idx ON drama_batches(project_id, batch_number);
COMMENT ON TABLE drama_batches IS '漫剧剧本解析批次及其在完整原文中的字符范围';
COMMENT ON COLUMN drama_batches.id IS '批次唯一标识';
COMMENT ON COLUMN drama_batches.project_id IS '所属漫剧项目标识';
COMMENT ON COLUMN drama_batches.batch_number IS '项目内顺序批次编号';
COMMENT ON COLUMN drama_batches.start_offset IS '原文起点，按 Unicode 字符计数，从零开始且包含此位置';
COMMENT ON COLUMN drama_batches.end_offset IS '原文终点，按 Unicode 字符计数，不包含此位置';
COMMENT ON COLUMN drama_batches.selection_mode IS '起点选择方式：从头、承接已有批次或手动选择';
COMMENT ON COLUMN drama_batches.previous_batch_id IS '承接起点时引用的前一批次标识';
COMMENT ON COLUMN drama_batches.boundary IS '终点来源：分集、章节、段落、五万字上限、原文末尾或手动';
COMMENT ON COLUMN drama_batches.status IS '批次状态：待解析、范围需核对或解析完成';
COMMENT ON COLUMN drama_batches.revision IS '批次范围乐观锁版本';
COMMENT ON COLUMN drama_batches.created_at IS '批次创建时间';
COMMENT ON COLUMN drama_batches.updated_at IS '批次最近修改时间';

-- +goose Down
DROP TABLE drama_batches;
