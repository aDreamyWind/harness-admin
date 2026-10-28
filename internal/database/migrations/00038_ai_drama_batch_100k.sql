-- +goose Up
ALTER TABLE drama_batches DROP CONSTRAINT drama_batches_check;
ALTER TABLE drama_batches ADD CONSTRAINT drama_batches_end_offset_check
    CHECK (end_offset > start_offset AND end_offset - start_offset <= 100000);
COMMENT ON COLUMN drama_batches.end_offset IS '原文终点，按 Unicode 字符计数，不包含此位置；单批最多十万字符';
COMMENT ON COLUMN drama_batches.boundary IS '终点来源：分集、章节、段落、十万字上限、原文末尾或手动';

-- +goose Down
ALTER TABLE drama_batches DROP CONSTRAINT drama_batches_end_offset_check;
ALTER TABLE drama_batches ADD CONSTRAINT drama_batches_check
    CHECK (end_offset > start_offset AND end_offset - start_offset <= 50000);
COMMENT ON COLUMN drama_batches.end_offset IS '原文终点，按 Unicode 字符计数，不包含此位置';
COMMENT ON COLUMN drama_batches.boundary IS '终点来源：分集、章节、段落、五万字上限、原文末尾或手动';
