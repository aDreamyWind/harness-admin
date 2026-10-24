package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/skilladmin"
)

type SkillRepository struct{ pool *pgxpool.Pool }

func NewSkillRepository(pool *pgxpool.Pool) *SkillRepository { return &SkillRepository{pool: pool} }

const skillAssetColumns = `s.id, s.name, s.description, s.status, s.published_version_id,
    COALESCE((SELECT MAX(v.version_no) FROM enterprise_skill_versions v WHERE v.skill_id = s.id), 0),
    ARRAY(SELECT g.department_id::text FROM enterprise_skill_grants g WHERE g.skill_id = s.id AND g.department_id IS NOT NULL ORDER BY g.department_id),
    ARRAY(SELECT g.user_id::text FROM enterprise_skill_grants g WHERE g.skill_id = s.id AND g.user_id IS NOT NULL ORDER BY g.user_id),
    s.created_at, s.updated_at`

func scanSkillAsset(row pgx.Row) (skilladmin.Asset, error) {
	var item skilladmin.Asset
	err := row.Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.PublishedVersionID,
		&item.LatestVersionNo, &item.DepartmentIDs, &item.UserIDs, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return skilladmin.Asset{}, skilladmin.ErrNotFound
	}
	return item, err
}

func getSkillAsset(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, id string) (skilladmin.Asset, error) {
	return scanSkillAsset(q.QueryRow(ctx, `SELECT `+skillAssetColumns+`
        FROM enterprise_skills s WHERE s.organization_id = $1 AND s.id = $2`, orgID, id))
}

func (r *SkillRepository) ImportVersion(ctx context.Context, actor skilladmin.Actor, pack skilladmin.Package) (skilladmin.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	defer tx.Rollback(ctx)
	id := uuid.NewString()
	err = tx.QueryRow(ctx, `INSERT INTO enterprise_skills (id,organization_id,name,description,created_by)
        VALUES ($1,$2,$3,$4,$5)
        ON CONFLICT (organization_id,name) DO UPDATE SET description = EXCLUDED.description, updated_at = NOW()
        RETURNING id`, id, actor.OrganizationID, pack.Name, pack.Description, actor.UserID).Scan(&id)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	var versionNo int
	var previousDigest *string
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_no),0),
        (SELECT manifest_digest FROM enterprise_skill_versions WHERE skill_id=$1 ORDER BY version_no DESC LIMIT 1)
        FROM enterprise_skill_versions WHERE skill_id=$1`, id).Scan(&versionNo, &previousDigest)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	if previousDigest != nil && *previousDigest == pack.ManifestDigest {
		return skilladmin.Asset{}, skilladmin.ErrConflict
	}
	versionID := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO enterprise_skill_versions
        (id,skill_id,version_no,frontmatter,manifest_digest,created_by,source_type,source_ref,source_revision,source_path)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		versionID, id, versionNo+1, pack.Frontmatter, pack.ManifestDigest, actor.UserID,
		pack.SourceType, pack.SourceRef, pack.SourceRevision, pack.SourcePath)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	for _, file := range pack.Files {
		_, err = tx.Exec(ctx, `INSERT INTO enterprise_skill_files
            (version_id,path,content,mime_type,digest,size_bytes) VALUES ($1,$2,$3,$4,$5,$6)`,
			versionID, file.Path, file.Content, file.MIMEType, file.Digest, file.Size)
		if err != nil {
			return skilladmin.Asset{}, err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, SourceIP: actor.SourceIP, Action: "skill.version.import", ResourceType: "enterprise_skill",
		ResourceID: id, Result: "success", Detail: map[string]any{"version": versionNo + 1, "manifestDigest": pack.ManifestDigest,
			"sourceType": pack.SourceType, "sourceRef": pack.SourceRef, "sourceRevision": pack.SourceRevision, "sourcePath": pack.SourcePath}}); err != nil {
		return skilladmin.Asset{}, err
	}
	item, err := getSkillAsset(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return skilladmin.Asset{}, err
	}
	return item, nil
}

func (r *SkillRepository) List(ctx context.Context, orgID string) ([]skilladmin.Asset, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+skillAssetColumns+` FROM enterprise_skills s
        WHERE s.organization_id=$1 ORDER BY s.updated_at DESC, s.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]skilladmin.Asset, 0)
	for rows.Next() {
		item, err := scanSkillAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SkillRepository) Versions(ctx context.Context, orgID, skillID string) ([]skilladmin.Version, error) {
	if _, err := getSkillAsset(ctx, r.pool, orgID, skillID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,skill_id,version_no,frontmatter,manifest_digest,source_type,source_ref,source_revision,source_path,created_at
        FROM enterprise_skill_versions WHERE skill_id=$1 ORDER BY version_no DESC`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]skilladmin.Version, 0)
	for rows.Next() {
		var item skilladmin.Version
		if err := rows.Scan(&item.ID, &item.SkillID, &item.VersionNo, &item.Frontmatter, &item.ManifestDigest,
			&item.SourceType, &item.SourceRef, &item.SourceRevision, &item.SourcePath, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		files, err := r.versionFiles(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].Files = files
	}
	return items, nil
}

func (r *SkillRepository) versionFiles(ctx context.Context, versionID string) ([]skilladmin.File, error) {
	rows, err := r.pool.Query(ctx, `SELECT path,mime_type,digest,size_bytes FROM enterprise_skill_files WHERE version_id=$1 ORDER BY path`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]skilladmin.File, 0)
	for rows.Next() {
		var file skilladmin.File
		if err := rows.Scan(&file.Path, &file.MIMEType, &file.Digest, &file.Size); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (r *SkillRepository) ReadVersionFile(ctx context.Context, orgID, skillID, versionID, filePath string) (skilladmin.File, error) {
	var file skilladmin.File
	err := r.pool.QueryRow(ctx, `SELECT f.path,f.content,f.mime_type,f.digest,f.size_bytes
        FROM enterprise_skill_files f
        JOIN enterprise_skill_versions v ON v.id=f.version_id
        JOIN enterprise_skills s ON s.id=v.skill_id
        WHERE s.organization_id=$1 AND s.id=$2 AND v.id=$3 AND f.path=$4`,
		orgID, skillID, versionID, filePath).Scan(&file.Path, &file.Content, &file.MIMEType, &file.Digest, &file.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return skilladmin.File{}, skilladmin.ErrNotFound
	}
	return file, err
}

func (r *SkillRepository) Publish(ctx context.Context, actor skilladmin.Actor, skillID, versionID, status string) (skilladmin.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := getSkillAsset(ctx, tx, actor.OrganizationID, skillID); err != nil {
		return skilladmin.Asset{}, err
	}
	if status == "published" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM enterprise_skill_versions WHERE skill_id=$1 AND id=$2)`, skillID, versionID).Scan(&exists); err != nil {
			return skilladmin.Asset{}, err
		}
		if !exists {
			return skilladmin.Asset{}, skilladmin.ErrNotFound
		}
		_, err = tx.Exec(ctx, `UPDATE enterprise_skills SET status='published',published_version_id=$2,updated_at=NOW() WHERE id=$1`, skillID, versionID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE enterprise_skills SET status='disabled',updated_at=NOW() WHERE id=$1`, skillID)
	}
	if err != nil {
		return skilladmin.Asset{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, SourceIP: actor.SourceIP, Action: "skill.asset.publish", ResourceType: "enterprise_skill",
		ResourceID: skillID, Result: "success", Detail: map[string]any{"status": status, "versionId": versionID}}); err != nil {
		return skilladmin.Asset{}, err
	}
	item, err := getSkillAsset(ctx, tx, actor.OrganizationID, skillID)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return skilladmin.Asset{}, err
	}
	return item, nil
}

func (r *SkillRepository) ReplaceGrants(ctx context.Context, actor skilladmin.Actor, skillID string, departments, users []string) (skilladmin.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := getSkillAsset(ctx, tx, actor.OrganizationID, skillID); err != nil {
		return skilladmin.Asset{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM enterprise_skill_grants WHERE skill_id=$1`, skillID); err != nil {
		return skilladmin.Asset{}, err
	}
	for _, departmentID := range departments {
		tag, err := tx.Exec(ctx, `INSERT INTO enterprise_skill_grants(id,skill_id,department_id)
            SELECT $1,$2,d.id FROM departments d WHERE d.id=$3 AND d.organization_id=$4 AND d.status='active'
            ON CONFLICT DO NOTHING`, uuid.NewString(), skillID, departmentID, actor.OrganizationID)
		if err != nil {
			return skilladmin.Asset{}, err
		}
		if tag.RowsAffected() != 1 {
			return skilladmin.Asset{}, skilladmin.ErrInvalidInput
		}
	}
	for _, userID := range users {
		tag, err := tx.Exec(ctx, `INSERT INTO enterprise_skill_grants(id,skill_id,user_id)
            SELECT $1,$2,u.id FROM users u WHERE u.id=$3 AND u.organization_id=$4 AND u.status='active'
            ON CONFLICT DO NOTHING`, uuid.NewString(), skillID, userID, actor.OrganizationID)
		if err != nil {
			return skilladmin.Asset{}, err
		}
		if tag.RowsAffected() != 1 {
			return skilladmin.Asset{}, skilladmin.ErrInvalidInput
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, SourceIP: actor.SourceIP, Action: "skill.asset.grants.replace", ResourceType: "enterprise_skill",
		ResourceID: skillID, Result: "success", Detail: map[string]any{"departmentCount": len(departments), "userCount": len(users)}}); err != nil {
		return skilladmin.Asset{}, err
	}
	item, err := getSkillAsset(ctx, tx, actor.OrganizationID, skillID)
	if err != nil {
		return skilladmin.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return skilladmin.Asset{}, err
	}
	return item, nil
}

const skillEmployeePermission = `EXISTS (SELECT 1 FROM enterprise_skill_grants g
    WHERE g.skill_id=s.id AND (g.user_id=u.id OR (g.department_id=u.department_id AND
        EXISTS (SELECT 1 FROM departments d WHERE d.id=u.department_id AND d.status='active'))))`

func (r *SkillRepository) ListForUser(ctx context.Context, orgID, userID string) ([]skilladmin.Published, error) {
	rows, err := r.pool.Query(ctx, `SELECT s.name FROM enterprise_skills s
        JOIN users u ON u.id=$2 AND u.organization_id=$1 AND u.status='active'
        WHERE s.organization_id=$1 AND s.status='published' AND s.published_version_id IS NOT NULL AND `+skillEmployeePermission+`
        ORDER BY s.name`, orgID, userID)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := make([]skilladmin.Published, 0, len(names))
	for _, name := range names {
		item, err := r.GetForUser(ctx, orgID, userID, name)
		if errors.Is(err, skilladmin.ErrForbidden) {
			continue
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *SkillRepository) GetForUser(ctx context.Context, orgID, userID, name string) (skilladmin.Published, error) {
	var item skilladmin.Published
	err := r.pool.QueryRow(ctx, `SELECT `+skillAssetColumns+`,
        v.id,v.skill_id,v.version_no,v.frontmatter,v.manifest_digest,v.source_type,v.source_ref,v.source_revision,v.source_path,v.created_at
        FROM enterprise_skills s JOIN enterprise_skill_versions v ON v.id=s.published_version_id
        JOIN users u ON u.id=$2 AND u.organization_id=$1 AND u.status='active'
        WHERE s.organization_id=$1 AND s.name=$3 AND s.status='published' AND `+skillEmployeePermission,
		orgID, userID, name).Scan(&item.Asset.ID, &item.Asset.Name, &item.Asset.Description, &item.Asset.Status,
		&item.Asset.PublishedVersionID, &item.Asset.LatestVersionNo, &item.Asset.DepartmentIDs, &item.Asset.UserIDs,
		&item.Asset.CreatedAt, &item.Asset.UpdatedAt, &item.Version.ID, &item.Version.SkillID, &item.Version.VersionNo,
		&item.Version.Frontmatter, &item.Version.ManifestDigest, &item.Version.SourceType, &item.Version.SourceRef,
		&item.Version.SourceRevision, &item.Version.SourcePath, &item.Version.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return skilladmin.Published{}, skilladmin.ErrForbidden
	}
	if err != nil {
		return skilladmin.Published{}, err
	}
	item.Asset.DepartmentIDs = nil
	item.Asset.UserIDs = nil
	var publishedFrontmatter struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(item.Version.Frontmatter, &publishedFrontmatter); err != nil {
		return skilladmin.Published{}, err
	}
	item.Asset.Description = publishedFrontmatter.Description
	item.Asset.LatestVersionNo = item.Version.VersionNo
	item.Version.Files, err = r.versionFiles(ctx, item.Version.ID)
	return item, err
}

func (r *SkillRepository) ReadFile(ctx context.Context, orgID, userID, name, filePath string) (skilladmin.File, error) {
	var file skilladmin.File
	err := r.pool.QueryRow(ctx, `SELECT f.path,f.content,f.mime_type,f.digest,f.size_bytes
        FROM enterprise_skills s JOIN enterprise_skill_files f ON f.version_id=s.published_version_id
        JOIN users u ON u.id=$2 AND u.organization_id=$1 AND u.status='active'
        WHERE s.organization_id=$1 AND s.name=$3 AND s.status='published' AND f.path=$4 AND `+skillEmployeePermission,
		orgID, userID, name, filePath).Scan(&file.Path, &file.Content, &file.MIMEType, &file.Digest, &file.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return skilladmin.File{}, skilladmin.ErrForbidden
	}
	if err != nil {
		return skilladmin.File{}, fmt.Errorf("读取 Skill 文件失败: %w", err)
	}
	return file, nil
}

var _ skilladmin.Repository = (*SkillRepository)(nil)
