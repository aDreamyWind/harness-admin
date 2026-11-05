package postgres

// 资产只存来源批次主键；展示名称通过批次表读取，改名不会留下旧文案。
const dramaAssetSourceSQL = `(SELECT jsonb_build_object('id',b.id,'name',b.name,'number',b.batch_number)
 FROM drama_batches b WHERE b.id=a.origin_batch_id AND b.project_id=a.project_id)`
const dramaAssetUsageSQL = `COALESCE((SELECT jsonb_agg(jsonb_build_object('id',used.id,'name',used.name,'number',used.batch_number) ORDER BY used.batch_number,used.id)
 FROM (SELECT DISTINCT b.id,b.name,b.batch_number FROM drama_episode_asset_links l
 JOIN drama_script_episodes e ON e.id=l.episode_id JOIN drama_batches b ON b.id=e.batch_id
 WHERE l.asset_id=a.id AND b.project_id=a.project_id) used),'[]'::jsonb)`
const dramaAssetJSON = `jsonb_build_object('id',a.id,'kind',a.kind,'name',a.name,'description',a.description,
 'appearanceDescription',a.appearance_description,'voiceDescription',a.voice_description,
 'sourceBatch',` + dramaAssetSourceSQL + `,'usedInBatches',` + dramaAssetUsageSQL + `)`
const dramaAssetJSONWithPreview = dramaAssetJSON + ` || COALESCE((
 SELECT jsonb_build_object('previewImageUrl','/api/v1/ai-drama/projects/' || a.project_id::text || '/images/' || j.id::text || '/content')
 FROM drama_asset_forms f JOIN drama_image_jobs j ON j.id=f.current_image_id AND j.form_id=f.id
 WHERE f.asset_id=a.id AND j.status='succeeded' AND j.storage_key<>''
 ORDER BY (f.parent_id IS NULL) DESC,j.updated_at DESC,j.id DESC LIMIT 1), '{}'::jsonb)`
const dramaAssetSelect = `a.id,a.kind,a.name,a.description,a.appearance_description,a.voice_description,` + dramaAssetSourceSQL + `,` + dramaAssetUsageSQL
