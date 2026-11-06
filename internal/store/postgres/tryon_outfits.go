package postgres

import (
	"context"

	"harness-admin/internal/tryon"
)

func (r *TryOnRepository) ListOutfits(ctx context.Context, owner tryon.Owner) ([]tryon.Outfit, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, garment_ids::text[], created_at FROM tryon_outfits
		WHERE organization_id = $1 AND owner_id = $2 ORDER BY created_at DESC, id DESC`,
		owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	outfits := make([]tryon.Outfit, 0)
	for rows.Next() {
		var outfit tryon.Outfit
		if err := rows.Scan(&outfit.ID, &outfit.Name, &outfit.GarmentIDs, &outfit.CreatedAt); err != nil {
			return nil, err
		}
		outfits = append(outfits, outfit)
	}
	return outfits, rows.Err()
}

func (r *TryOnRepository) CreateOutfit(ctx context.Context, owner tryon.Owner, outfit tryon.Outfit, builtinAssets []tryon.AssetImage) (tryon.Outfit, error) {
	if len(outfit.GarmentIDs) < 2 || len(outfit.GarmentIDs) > tryon.MaxTryOnGarments {
		return tryon.Outfit{}, tryon.ErrInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return tryon.Outfit{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockTryOnOwner(ctx, tx, owner); err != nil {
		return tryon.Outfit{}, err
	}
	if err := persistTryOnAssets(ctx, tx, owner, outfit.GarmentIDs, builtinAssets); err != nil {
		return tryon.Outfit{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO tryon_outfits (id, organization_id, owner_id, name, garment_ids)
		VALUES ($1, $2, $3, $4, $5::text[]::uuid[]) RETURNING created_at`,
		outfit.ID, owner.OrganizationID, owner.UserID, outfit.Name, outfit.GarmentIDs).Scan(&outfit.CreatedAt)
	if err != nil {
		return tryon.Outfit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tryon.Outfit{}, err
	}
	return outfit, nil
}

func (r *TryOnRepository) DeleteOutfit(ctx context.Context, owner tryon.Owner, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tryon_outfits WHERE organization_id = $1 AND owner_id = $2 AND id = $3`,
		owner.OrganizationID, owner.UserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return tryon.ErrNotFound
	}
	return nil
}
