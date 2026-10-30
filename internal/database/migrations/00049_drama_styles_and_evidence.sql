-- +goose Up
CREATE TABLE drama_visual_styles (
 id varchar(80) PRIMARY KEY,
 name varchar(120) NOT NULL UNIQUE,
 category varchar(16) NOT NULL,
 preview_url text NOT NULL,
 preview_x integer NOT NULL CHECK(preview_x BETWEEN 0 AND 2),
 preview_y integer NOT NULL CHECK(preview_y BETWEEN 0 AND 2),
 image_prompt text NOT NULL,
 video_prompt text NOT NULL,
 sort_order integer NOT NULL DEFAULT 0,
 enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE drama_visual_styles IS '漫剧统一视觉风格目录，公开展示信息与服务端提示词分列保存';
COMMENT ON COLUMN drama_visual_styles.id IS '稳定风格编码，业务接口仅提交此编码';
COMMENT ON COLUMN drama_visual_styles.name IS '前端展示的风格名称';
COMMENT ON COLUMN drama_visual_styles.category IS '前端分类：真人、二维、三维或历史风格';
COMMENT ON COLUMN drama_visual_styles.preview_url IS '公开展示的预览图地址';
COMMENT ON COLUMN drama_visual_styles.preview_x IS '预览图集横向索引，从零开始';
COMMENT ON COLUMN drama_visual_styles.preview_y IS '预览图集纵向索引，从零开始';
COMMENT ON COLUMN drama_visual_styles.image_prompt IS '仅服务端使用的生图风格提示词，不通过普通业务接口返回';
COMMENT ON COLUMN drama_visual_styles.video_prompt IS '仅服务端使用的生视频风格提示词，供后续视频生成接入';
COMMENT ON COLUMN drama_visual_styles.sort_order IS '风格展示顺序';
COMMENT ON COLUMN drama_visual_styles.enabled IS '是否可选择和用于新生成任务';
COMMENT ON COLUMN drama_visual_styles.created_at IS '风格创建时间';
COMMENT ON COLUMN drama_visual_styles.updated_at IS '风格更新时间';
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('cinematic-realism','电影写实','真人','/drama-style-sheet.png',0,0,'电影摄影质感，真实材质和自然肤色，可信的空间光照，克制的色彩分级，细腻的皮肤与织物细节。','保持写实电影摄影与自然动作，光线和材质跨镜头连续，人物面部与服装保持一致。',0);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('ink-wash','国风水墨','2D','/drama-style-sheet.png',1,0,'东方水墨绘画，宣纸纹理，浓淡墨色，留白构图，柔和晕染，简练而有韵律的线条，雅致低饱和设色。','保持水墨线条、留白和宣纸质感，动作节奏含蓄流畅，水墨晕染稳定，不变为摄影写实。',1);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('anime-2d','日系动漫','2D','/drama-style-sheet.png',2,0,'精致二维动画设定，清晰线稿，赛璐璐分层上色，稳定的人体比例，明确的轮廓和表情，干净的色块。','保持二维动画线稿和赛璐璐阴影，角色比例与色块稳定，表情和肢体动作清晰连贯。',2);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('animation-3d','三维动画','3D','/drama-style-sheet.png',0,1,'高质量三维动画，精细建模，柔和全局照明，体积感清晰，材质层次丰富，适度风格化的造型。','保持三维动画建模、材质与灯光一致，运动自然平滑，维持角色体积和服装结构。',3);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('dark-comic','暗黑漫画','2D','/drama-style-sheet.png',1,1,'暗黑叙事漫画，黑白灰主导，强烈明暗对照，密集排线与手绘笔触，厚重阴影，克制的局部色彩。','保持暗黑漫画排线和黑白灰层次，光影方向一致，动作轮廓清楚，不增加无依据的惊悚情节。',4);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('fantasy-picturebook','奇幻绘本','2D','/drama-style-sheet.png',2,1,'奇幻绘本插画，柔和手绘纹理，富有想象力但结构可信的造型，温润色彩，细腻环境层次。','保持绘本手绘纹理与柔和色彩，画面运动舒缓连贯，人物造型与场景关系稳定。',5);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('american-comic','美式漫画','2D','/drama-style-sheet.png',0,2,'美式叙事漫画，硬朗墨线，明确体块，高对比色块，戏剧性光影，清晰轮廓与有力构图。','保持硬朗墨线和高对比色块，人物动作有重量感，构图清晰，角色服装与轮廓连续。',6);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('retro-animation','怀旧动画','2D','/drama-style-sheet.png',1,2,'复古二维动画，手绘轮廓，温暖而克制的旧胶片配色，轻微颗粒，简洁形状与平涂背景。','保持复古手绘线条与胶片颗粒，角色动作节奏自然，避免逐帧画风和颜色跳变。',7);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('cyber-neon','赛博霓虹','真人','/drama-style-sheet.png',2,2,'赛博霓虹视觉，深色环境中有节制的青紫与洋红光源，金属和玻璃的真实反射，清晰的主体轮廓，细致材质。','保持青紫霓虹光照方向与真实反射，运动连续，人物身份与环境结构稳定。',8);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('dark-concept','黑暗原画概念风','2D','/drama-style-sheet.png',1,1,'黑暗奇幻概念原画，写实造型结合厚涂笔触，低明度低饱和配色，冷暖克制，深邃阴影，可信材质与清晰剪影，丰富但有主次的细节。','保持厚涂概念原画的笔触、低明度色调与清晰剪影，人物五官、材质和光影方向跨镜头稳定。',9);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('dark-fantasy-illustration','黑暗奇幻插画风','2D','/drama-style-sheet.png',2,1,'黑暗奇幻插画，深绿色和灰蓝色环境光，精细手绘质感，复杂自然纹理，幽暗但可辨识的层次，主体明暗关系清晰。','保持黑暗奇幻插画的手绘纹理和幽暗层次，动作自然，角色与场景细节保持一致。',10);
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order) VALUES('retro-halftone','复古半色调','2D','/drama-style-sheet.png',0,2,'复古半色调漫画，印刷网点，有限色版，粗墨线，略带纸张颗粒，高对比但层次清晰的构图。','保持复古印刷网点与有限色版，运动轮廓清晰，网点纹理稳定，不随帧闪烁。',11);

-- 历史自定义名称保留为可选风格，不丢失已有项目的选择。
INSERT INTO drama_visual_styles(id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt,sort_order)
SELECT 'legacy-'||md5(legacy.name),legacy.name,'历史','/drama-style-sheet.png',0,0,
 '保持统一视觉风格：'||legacy.name||'。主体结构清晰，材质和光影一致。',
 '保持统一视觉风格：'||legacy.name||'。角色造型、场景与运动连续。',100
FROM (SELECT style AS name FROM drama_projects UNION SELECT style AS name FROM drama_asset_forms) legacy
WHERE trim(legacy.name)<>'' AND NOT EXISTS(SELECT 1 FROM drama_visual_styles s WHERE s.name=legacy.name);
ALTER TABLE drama_projects ADD COLUMN style_id varchar(80) REFERENCES drama_visual_styles(id);
ALTER TABLE drama_asset_forms ADD COLUMN style_id varchar(80) REFERENCES drama_visual_styles(id);
ALTER TABLE drama_image_jobs ADD COLUMN style_id varchar(80) REFERENCES drama_visual_styles(id);
COMMENT ON COLUMN drama_projects.style_id IS '项目默认视觉风格目录主键，历史兼容数据可为空';
COMMENT ON COLUMN drama_asset_forms.style_id IS '形象使用的视觉风格主键，可独立于项目选择';
COMMENT ON COLUMN drama_image_jobs.style_id IS '生成时使用的视觉风格主键，实际提示词另存任务快照';
UPDATE drama_projects p SET style_id=s.id FROM drama_visual_styles s WHERE s.name=p.style;
UPDATE drama_asset_forms f SET style_id=s.id FROM drama_visual_styles s WHERE s.name=f.style;
UPDATE drama_asset_forms f SET style_id=p.style_id FROM drama_assets a JOIN drama_projects p ON p.id=a.project_id WHERE f.asset_id=a.id AND f.style_id IS NULL;
UPDATE drama_image_jobs j SET style_id=f.style_id FROM drama_asset_forms f WHERE j.form_id=f.id;
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','evidence','context','plan','name','director','review','repair'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术、剧情整理组、原文证据单元、本批上下文、规划、命名、导演、审校或修正';

-- +goose Down
DELETE FROM drama_batch_steps WHERE stage='evidence';
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','context','plan','name','director','review','repair'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术、原文证据、本批上下文、分集规划、命名、导演、审校、局部修正';
ALTER TABLE drama_image_jobs DROP COLUMN style_id;
ALTER TABLE drama_asset_forms DROP COLUMN style_id;
ALTER TABLE drama_projects DROP COLUMN style_id;
DROP TABLE drama_visual_styles;
