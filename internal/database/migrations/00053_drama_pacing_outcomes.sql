-- +goose Up
ALTER TABLE drama_repair_tickets DROP CONSTRAINT drama_repair_tickets_status_check;
ALTER TABLE drama_repair_tickets ADD CONSTRAINT drama_repair_tickets_status_check CHECK(status IN ('assigned','resolved','feedback','blocked','declined'));
ALTER TABLE drama_repair_tickets
 ADD COLUMN before_shot_count integer CHECK(before_shot_count>0),
 ADD COLUMN after_shot_count integer CHECK(after_shot_count>0),
 ADD COLUMN before_duration double precision CHECK(before_duration>0),
 ADD COLUMN after_duration double precision CHECK(after_duration>0);
COMMENT ON TABLE drama_repair_tickets IS '漫剧中心调度派工、修正回路及节奏优化采纳结果，不保存模型思考';
COMMENT ON COLUMN drama_repair_tickets.status IS '已派工、已解决、反馈仍有问题、需人工或未采纳并保留原稿';
COMMENT ON COLUMN drama_repair_tickets.before_shot_count IS '节奏优化前实际镜头数，其他派工为空';
COMMENT ON COLUMN drama_repair_tickets.after_shot_count IS '节奏优化结束后实际保存的镜头数，未采纳时等于原稿';
COMMENT ON COLUMN drama_repair_tickets.before_duration IS '节奏优化前实际规划总秒数，其他派工为空';
COMMENT ON COLUMN drama_repair_tickets.after_duration IS '节奏优化结束后实际保存的规划总秒数，未采纳时等于原稿';
-- +goose Down
UPDATE drama_repair_tickets SET status='blocked' WHERE status='declined';
ALTER TABLE drama_repair_tickets DROP CONSTRAINT drama_repair_tickets_status_check;
ALTER TABLE drama_repair_tickets ADD CONSTRAINT drama_repair_tickets_status_check CHECK(status IN ('assigned','resolved','feedback','blocked'));
ALTER TABLE drama_repair_tickets DROP COLUMN before_shot_count,DROP COLUMN after_shot_count,DROP COLUMN before_duration,DROP COLUMN after_duration;
