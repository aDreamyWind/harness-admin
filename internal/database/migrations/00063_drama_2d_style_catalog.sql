-- +goose Up
ALTER TABLE drama_visual_styles ADD COLUMN catalog_name varchar(120);
COMMENT ON TABLE drama_visual_styles IS '漫剧视觉风格，区分可选目录与仍可解析的历史风格';
COMMENT ON COLUMN drama_visual_styles.catalog_name IS '当前风格库的展示名称，空值表示只兼容历史引用';

-- 历史名称仍用于解析旧项目，目录以附件的二维风格名称和顺序为准。
WITH presets (id, legacy_name, catalog_name, preview_url, preview_x, preview_y, image_prompt, video_prompt, sort_order) AS (
 VALUES
 ('pixel-art', '像素风', '像素风', '/drama-styles/sheet-1.jpg', 0, 0, '低分辨率手工像素绘画，边缘沿方形像素阶梯，限定色板、抖色阴影和清晰剪影，像素颗粒大小一致。', '保持像素网格和色板一致，用清楚的逐帧姿势表现动作，镜头移动贴合像素网格。', 0),
 ('ink-wash', '国风水墨', '东方水墨画风', '/drama-styles/sheet-1.jpg', 1, 0, '宣纸上的中国水墨，墨色渗化、干笔飞白和远近浓淡，少量浅绛设色与大面积留白。', '保持宣纸纤维和墨色层次，留白随构图自然移动，晕染边界稳定。', 1),
 ('retro-halftone', '复古半色调', '复古半色调', '/drama-styles/sheet-1.jpg', 2, 0, '双色复古印刷漫画，粗墨轮廓、醒目的网点阴影、旧报纸底色和套色错位。', '稳定印刷网点的位置、尺寸与粗墨边线，色版随镜头保持一致，避免闪烁。', 2),
 ('minimal-illustration', '简洁插画风', '简洁插画风', '/drama-styles/sheet-1.jpg', 0, 1, '极简编辑插画，大色面几何形、留白和单色细线，仅用少量纯色突出主体与动作。', '维持几何形与限定色板，动作和转场简明，画面密度保持克制。', 3),
 ('dark-comic', '暗黑漫画', '暗黑漫画风', '/drama-styles/sheet-1.jpg', 1, 1, '黑白悬疑漫画，大面积浓黑、斜向排线和刮白反光，棱角剪影与强烈明暗对比。', '保持黑白墨块与排线的密度，动作主要靠可读剪影，跨镜头保持阴影方向。', 4),
 ('dark-fantasy-illustration', '黑暗奇幻插画风', '黑暗奇幻插画风', '/drama-styles/sheet-1.jpg', 2, 1, '幽暗奇幻手绘插画，灰蓝与苔绿的空气透视、细密草木及岩纹，局部微光衬出主体。', '保持冷色环境和幽微光源，风与雾缓慢流动，草木岩石的纹理与明暗稳定。', 5),
 ('ghibli-healing', '吉卜力治愈动画', '吉卜力治愈动画', '/drama-styles/sheet-1.jpg', 0, 2, '怀旧治愈系手绘二维动画，自然光、温润草木色与水粉风景，人物表情细腻，背景保留生活细节。', '用轻柔的表情和自然小动作表现生活气息，保持水粉背景、自然光和人物线稿一致。', 6),
 ('american-dark-comic', '美式黑暗插画风', '美式黑暗插画风', '/drama-styles/sheet-4.jpg', 1, 1, '美式黑色漫画插画，厚重黑墨、尖锐交叉排线、高对比边缘光，局部暗红点色。', '保持夸张黑色阴影、粗硬轮廓与克制的暗红色板，动作姿势干净有重量。', 7),
 ('shadow-puppet-illustration', '皮影戏插画风', '皮影戏插画风', '/drama-styles/sheet-1.jpg', 2, 2, '中国皮影戏插画，半透明彩皮、镂刻纹样、关节式侧影和背后暖光，轮廓平面化。', '人物用皮影铰接式姿态运动，保持半透明彩皮、镂空纹样与稳定背光。', 8),
 ('dark-concept', '黑暗原画概念风', '黑暗原画概念风', '/drama-styles/sheet-2.jpg', 0, 0, '黑暗奇幻概念原画，宽幅构图、可信结构与比例、低饱和冷暖光，厚涂边缘形成清晰剪影。', '延续低明度厚涂、建筑与角色结构，保持光源方向及主体的清晰剪影。', 9),
 ('dark-impasto-illustration', '暗黑调厚涂风插画风', '暗黑调厚涂风插画风', '/drama-styles/sheet-2.jpg', 1, 0, '暗调厚涂二维插画，刮刀堆叠、稠厚油彩、粗糙高光和赭紫黑色层，边缘笔触富有颗粒。', '保留厚涂笔触的形状和分层，冷暖阴影连续，局部高光跟随同一光源变化。', 10),
 ('tezuka-retro-cartoon', '手冢治虫时代卡通画风', '手冢治虫时代卡通画风', '/drama-styles/sheet-2.jpg', 2, 0, '昭和早期卡通语言，圆润头型与大眼、柔软四肢、干净墨线、稀疏背景与旧纸双色上色。', '保持圆润比例与双色线稿，动作富有轻快弹性，五官随表演变化仍易于辨认。', 11),
 ('shanghai-animation-style', '上美画风', '上美画风', '/drama-styles/sheet-4.jpg', 1, 2, '传统中国手绘动画，美术片式平面装饰、工笔勾线、矿物色和平视或散点透视。', '保持工笔线条、平面装饰与中国传统配色，用戏曲式身段与自然节奏表现动作。', 12),
 ('chinese-mythology', '中国神话风', '中国神话风', '/drama-styles/sheet-2.jpg', 1, 1, '中国神话二维插画，壁画式云纹、金粉与石青朱砂，飘带和传统纹样形成恢宏节奏。', '稳定石青朱砂和金线纹样，云气与飘带方向和光源一致，神话人物气势清晰。', 13),
 ('two-dimensional-cartoon', '二维卡通插画风', '二维卡通插画风', '/drama-styles/sheet-2.jpg', 2, 1, '现代二维卡通，鲜明亮色、软圆轮廓和简洁色面，适度夸张表情与饱满剪影。', '使用夸张但清楚的关键姿势，保持亮色块和圆润轮廓，运动连续。', 14),
 ('otomo-animation', '大友克洋风', '大友克洋风', '/drama-styles/sheet-4.jpg', 2, 1, '复古日本科幻动画，细密手绘机械结构、旧城市纹理、暗红配工业蓝灰、冷峻透视和赛璐璐明暗。', '机车和机械结构运动符合形体与透视，稳定赛璐璐层次与工业配色，保留密集背景细节。', 15),
 ('shonen-battle-manga', '高质量2D热血漫风', '高质量2D热血漫风', '/drama-styles/sheet-2.jpg', 1, 2, '高动势少年热血漫画，强透视姿势、放射速度线、粗细分明的墨线和有冲击力的高纯度色块。', '用预备姿势、爆发和落点表现动作，速度线服从运动方向，保持线稿和色块稳定。', 16),
 ('monochrome-manga-animation', '黑白二维漫画动画风', '黑白二维漫画动画风', '/drama-styles/sheet-2.jpg', 2, 2, '黑白二维漫画动画，纯黑笔线、明亮留白、细网点与分明黑白色块，灰度干净。', '保持纯黑线稿和留白，每帧网点密度稳定，以形体和镜头节奏表现情绪。', 17),
 ('children-crayon-illustration', '儿童蜡笔手绘插画风', '儿童蜡笔手绘插画风', '/drama-styles/sheet-3.jpg', 0, 0, '儿童蜡笔手绘，稚拙不规则比例、蜡笔颗粒、叠涂边缘、柔和亮色和可见白纸纹理。', '保持蜡笔颗粒与不规则轮廓，节奏轻快，蜡笔色块连续移动而不漂移。', 18),
 ('retro-psychedelic-texture', '复古肌理迷幻插画风', '复古肌理迷幻插画风', '/drama-styles/sheet-3.jpg', 1, 0, '复古迷幻印刷插画，旋涡、同心波纹、流动有机形与饱和橙紫，加入丝网颗粒与套色质感。', '图案以缓慢可读的旋转或变形运动，锁定丝网肌理与印刷色版，避免色彩闪烁。', 19),
 ('game-concept-art', '游戏概念艺术风', '游戏概念艺术风', '/drama-styles/sheet-3.jpg', 2, 0, '二维游戏环境概念绘画，纵深与可辨识路径、前中后景色阶、明确地点功能和整洁材质设计。', '镜头移动展示环境布局并保留通行路径，前中后景层次和材质逻辑跨镜头统一。', 20),
 ('chinese-anime', '国漫二次元常用风', '国漫二次元常用风', '/drama-styles/sheet-3.jpg', 0, 1, '当代国漫二维画风，清秀比例、利落线条、青玉与朱红点色，服装与背景保留东方设计元素。', '保持国漫画风的人物线稿和服装结构，场景配色一致，动作连贯有力度。', 21),
 ('dali-surrealism', '达利风', '达利风', '/drama-styles/sheet-3.jpg', 1, 1, '超现实主义绘画，平静荒原与不可能的悬浮、熔融和比例变形，锐利长阴影与清澈天空。', '让超现实变化循序发生，清楚交代前后物体状态，维持画面透视和投影方向。', 22),
 ('monochrome-ink', '黑白水墨风', '黑白水墨风', '/drama-styles/sheet-3.jpg', 2, 1, '纯黑白水墨，积墨与淡墨形成黑白灰，湿笔渗化、枯笔飞白和层次分明的留白。', '保持黑白墨阶、枯湿笔触及留白，墨气随动作自然流动，构图简净。', 23),
 ('american-comic-animation', '美国漫画动画插画风', '美国漫画动画插画风', '/drama-styles/sheet-4.jpg', 0, 2, '美式漫画动画插画，鲜亮蓝红、清楚的肌肉和衣褶体块、粗墨轮廓及少量印刷网点。', '强调挺拔动作和漫画式重拍，保持粗线和清晰色块，跨镜头保持配色和比例。', 24),
 ('cyberpunk-digital-illustration', '赛博朋克数字插画风', '赛博朋克数字插画风', '/drama-styles/sheet-3.jpg', 1, 2, '二维赛博朋克数字插画，锐利平面图形、青紫洋红霓虹与高密度未来城市，反光用色块表现。', '霓虹灯牌、屏幕和倒影随透视运动，稳定青紫色域和二维绘画边缘。', 25),
 ('vaporwave-illustration', '插画蒸汽波风', '插画蒸汽波风', '/drama-styles/sheet-3.jpg', 2, 2, '蒸汽波二维插画，粉紫青渐变、复古电脑网格、夕阳色带和拼贴几何雕塑，低保真合成质感。', '保持渐变色带与复古网格一致，拼贴图形随景深缓动，色域不跳变。', 26),
 ('japanese-flat-illustration', '日系平涂插画风', '日系平涂插画风', '/drama-styles/sheet-4.jpg', 0, 0, '日系现代平涂插画，柔和低饱和配色、纤细轮廓、干净纯色平面和简化日常细节。', '保留细线与平涂色面，以自然微小动作推进，保持配色和造型简洁。', 27),
 ('american-game-concept-art', '美国游戏概念艺术风', '美国游戏概念艺术风', '/drama-styles/sheet-4.jpg', 1, 0, '美式游戏二维概念原画，宽阔构图、硬朗体块、冷暖体积光与清楚的装备结构，边缘笔触有张力。', '保持装备和场景结构统一，使用清晰透视和有重量感的动作，冷暖光方向延续。', 28),
 ('anime-concept-art', '二次元概念艺术风', '二次元概念艺术风', '/drama-styles/sheet-4.jpg', 2, 0, '二次元概念艺术，精致线稿结合分层手绘光影，强调造型设计、氛围光和清楚的前后景叙事。', '维持概念图的光色关系、衣饰轮廓和场景层次，动作保持二维动画质感。', 29),
 ('anime-2d', '日系动漫', '2D动漫', '/drama-styles/sheet-4.jpg', 0, 1, '标准二维赛璐璐动漫，清晰闭合线稿、两到三档平涂阴影、稳定人体比例和明确表情。', '保持二维线稿和赛璐璐色块不漂移，人物表情与肢体动作清楚连贯。', 30)
)
INSERT INTO drama_visual_styles (id, name, catalog_name, category, preview_url, preview_x, preview_y, image_prompt, video_prompt, sort_order)
SELECT id, CASE WHEN EXISTS (
    SELECT 1 FROM drama_visual_styles existing WHERE existing.name = presets.legacy_name AND existing.id <> presets.id
) THEN '2D 预设 ' || id ELSE legacy_name END,
    catalog_name, '2D', preview_url, preview_x, preview_y, image_prompt, video_prompt, sort_order
FROM presets
ON CONFLICT (id) DO UPDATE SET
    catalog_name = EXCLUDED.catalog_name,
    category = EXCLUDED.category,
    preview_url = EXCLUDED.preview_url,
    preview_x = EXCLUDED.preview_x,
    preview_y = EXCLUDED.preview_y,
    image_prompt = EXCLUDED.image_prompt,
    video_prompt = EXCLUDED.video_prompt,
    sort_order = EXCLUDED.sort_order,
    updated_at = now();

-- +goose Down
-- 已被项目、形象或任务引用的新增风格保留，便于降级后继续读取已有项目。
DELETE FROM drama_visual_styles s
WHERE s.catalog_name IS NOT NULL
  AND s.id NOT IN ('ink-wash', 'dark-comic', 'anime-2d', 'dark-fantasy-illustration', 'dark-concept', 'retro-halftone')
  AND NOT EXISTS (SELECT 1 FROM drama_projects p WHERE p.style_id = s.id)
  AND NOT EXISTS (SELECT 1 FROM drama_asset_forms f WHERE f.style_id = s.id)
  AND NOT EXISTS (SELECT 1 FROM drama_image_jobs j WHERE j.style_id = s.id);

UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=1, preview_y=0,
    image_prompt='东方水墨绘画，宣纸纹理，浓淡墨色，留白构图，柔和晕染，简练而有韵律的线条，雅致低饱和设色。',
    video_prompt='保持水墨线条、留白和宣纸质感，动作节奏含蓄流畅，水墨晕染稳定，不变为摄影写实。', sort_order=1, updated_at=now()
WHERE id='ink-wash';
UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=1, preview_y=1,
    image_prompt='暗黑叙事漫画，黑白灰主导，强烈明暗对照，密集排线与手绘笔触，厚重阴影，克制的局部色彩。',
    video_prompt='保持暗黑漫画排线和黑白灰层次，光影方向一致，动作轮廓清楚，不增加无依据的惊悚情节。', sort_order=4, updated_at=now()
WHERE id='dark-comic';
UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=2, preview_y=0,
    image_prompt='精致二维动画设定，清晰线稿，赛璐璐分层上色，稳定的人体比例，明确的轮廓和表情，干净的色块。',
    video_prompt='保持二维动画线稿和赛璐璐阴影，角色比例与色块稳定，表情和肢体动作清晰连贯。', sort_order=2, updated_at=now()
WHERE id='anime-2d';
UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=2, preview_y=1,
    image_prompt='黑暗奇幻插画，深绿色和灰蓝色环境光，精细手绘质感，复杂自然纹理，幽暗但可辨识的层次，主体明暗关系清晰。',
    video_prompt='保持黑暗奇幻插画的手绘纹理和幽暗层次，动作自然，角色与场景细节保持一致。', sort_order=10, updated_at=now()
WHERE id='dark-fantasy-illustration';
UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=1, preview_y=1,
    image_prompt='黑暗奇幻概念原画，写实造型结合厚涂笔触，低明度低饱和配色，冷暖克制，深邃阴影，可信材质与清晰剪影，丰富但有主次的细节。',
    video_prompt='保持厚涂概念原画的笔触、低明度色调与清晰剪影，人物五官、材质和光影方向跨镜头稳定。', sort_order=9, updated_at=now()
WHERE id='dark-concept';
UPDATE drama_visual_styles SET preview_url='/drama-style-sheet.png', preview_x=0, preview_y=2,
    image_prompt='复古半色调漫画，印刷网点，有限色版，粗墨线，略带纸张颗粒，高对比但层次清晰的构图。',
    video_prompt='保持复古印刷网点与有限色版，运动轮廓清晰，网点纹理稳定，不随帧闪烁。', sort_order=11, updated_at=now()
WHERE id='retro-halftone';

ALTER TABLE drama_visual_styles DROP COLUMN catalog_name;
COMMENT ON TABLE drama_visual_styles IS '漫剧统一视觉风格目录，公开展示信息与服务端提示词分列保存';
