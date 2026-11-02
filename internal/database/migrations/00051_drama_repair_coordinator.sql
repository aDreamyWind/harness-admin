-- +goose Up
CREATE TABLE drama_repair_tickets (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('storyboard','batch')),
 run_id uuid NOT NULL,
 item integer NOT NULL CHECK(item>=0),
 reporter text NOT NULL,
 problem text NOT NULL,
 state_hash text NOT NULL,
 node text NOT NULL,
 target_item integer NOT NULL CHECK(target_item>=0),
 reason text NOT NULL,
 instructions text NOT NULL,
 return_to text NOT NULL,
 status text NOT NULL CHECK(status IN ('assigned','resolved','feedback','blocked')),
 outcome text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE drama_repair_tickets IS '漫剧中心调度Agent的修正任务和回路记录，不保存模型思考';
COMMENT ON COLUMN drama_repair_tickets.id IS '修正派单主键';
COMMENT ON COLUMN drama_repair_tickets.project_id IS '所属项目';
COMMENT ON COLUMN drama_repair_tickets.kind IS '工作流类型：分镜或剧本';
COMMENT ON COLUMN drama_repair_tickets.run_id IS '不可变运行主键';
COMMENT ON COLUMN drama_repair_tickets.item IS '发现问题的片段或剧集编号，零表示总规划';
COMMENT ON COLUMN drama_repair_tickets.reporter IS '报告问题的检查节点';
COMMENT ON COLUMN drama_repair_tickets.problem IS '检查发现的具体问题';
COMMENT ON COLUMN drama_repair_tickets.state_hash IS '问题和输入状态摘要，用于无进展检测';
COMMENT ON COLUMN drama_repair_tickets.node IS '中心选择的责任节点或工具';
COMMENT ON COLUMN drama_repair_tickets.target_item IS '需要修正的上游片段或剧集编号';
COMMENT ON COLUMN drama_repair_tickets.reason IS '中心派工的归因依据';
COMMENT ON COLUMN drama_repair_tickets.instructions IS '交给节点的修正要求';
COMMENT ON COLUMN drama_repair_tickets.return_to IS '修正后必须返回的检查点';
COMMENT ON COLUMN drama_repair_tickets.status IS '已派工、已解决、反馈仍有问题或需人工';
COMMENT ON COLUMN drama_repair_tickets.outcome IS '修正及复核结果';
COMMENT ON COLUMN drama_repair_tickets.created_at IS '派工时间';
COMMENT ON COLUMN drama_repair_tickets.updated_at IS '复核更新时间';
CREATE INDEX drama_repair_ticket_scope ON drama_repair_tickets(project_id,kind,run_id,item,created_at);
-- +goose Down
DROP TABLE drama_repair_tickets;
