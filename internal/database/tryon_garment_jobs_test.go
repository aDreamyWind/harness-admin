package database

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/store/postgres"
	"harness-admin/internal/tryon"
)

func testGarmentJobPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	owner := tryon.Owner{OrganizationID: orgID}
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE organization_id = $1 AND username = 'admin'`, orgID).Scan(&owner.UserID); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewTryOnRepository(pool)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	source := tryon.Image{Data: buf.Bytes(), MIME: "image/png"}
	const model = "editor"
	box := tryon.Box{0.1, 0.2, 0.8, 0.9}
	items := []tryon.GarmentJobItem{
		{Name: "蓝色上衣", Category: "上衣", Box: &box},
		{Name: "白色裤子", Category: "裤子"},
	}
	first, err := repo.CreateGarmentJobs(ctx, owner, model, items, source)
	if err != nil || len(first) != 2 {
		t.Fatalf("首批任务：%+v，错误：%v", first, err)
	}
	added, err := repo.CreateGarmentJobs(ctx, owner, model, []tryon.GarmentJobItem{{Name: "灰色外套", Category: "外套"}}, source)
	if err != nil || len(added) != 1 {
		t.Fatalf("首批完成前应可追加：%+v，错误：%v", added, err)
	}
	var batchCount, taskCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(DISTINCT batch_id), COUNT(*) FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2`, owner.OrganizationID, owner.UserID).Scan(&batchCount, &taskCount)
	if err != nil || batchCount != 2 || taskCount != 3 {
		t.Fatalf("每批应共享一个原图，得到 %d 批 %d 件，错误：%v", batchCount, taskCount, err)
	}

	stranger := tryon.Owner{OrganizationID: orgID, UserID: uuid.NewString()}
	if other, err := repo.ListGarmentJobs(ctx, stranger); err != nil || len(other) != 0 {
		t.Fatalf("其他用户可看见当前衣物任务：%+v，错误：%v", other, err)
	}
	if _, err := repo.GarmentJobResult(ctx, stranger, first[0].ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("越权请求结果应视为不存在：%v", err)
	}
	if _, err := repo.SaveGarmentJob(ctx, stranger, first[0].ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("其他用户不能入库：%v", err)
	}
	if _, err := repo.RetryGarmentJob(ctx, stranger, first[0].ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("其他用户不能重试：%v", err)
	}
	if _, err := repo.GarmentJobResult(ctx, owner, first[0].ID); !errors.Is(err, tryon.ErrGarmentJobState) {
		t.Fatalf("排队任务尚无结果：%v", err)
	}

	type claimResult struct {
		work tryon.GarmentWork
		err  error
	}
	claims := make(chan claimResult, 2)
	for range 2 {
		go func() {
			work, err := repo.ClaimGarmentJob(ctx, time.Now().Add(time.Minute))
			claims <- claimResult{work, err}
		}()
	}
	active := make(map[string]tryon.GarmentWork)
	for range 2 {
		claim := <-claims
		if claim.err != nil {
			t.Fatal(claim.err)
		}
		work := claim.work
		if work.Job.Status != "running" || work.Owner != owner || !bytes.Equal(work.Source.Data, source.Data) || work.Source.MIME != source.MIME || work.Lease.Nanosecond()%1000 != 0 {
			t.Fatalf("任务未领取或未带上参考原图：%+v", work.Job)
		}
		active[work.Job.ID] = work
	}
	if len(active) != 2 || active[first[0].ID].Box == nil || *active[first[0].ID].Box != box || active[first[1].ID].Job.ID == "" {
		t.Fatalf("并发领取应该各得一件首批衣物，保留目标框：%+v", active)
	}
	if err := repo.FinishGarmentJob(ctx, first[0].ID, active[first[0].ID].Lease, source, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishGarmentJob(ctx, first[1].ID, active[first[1].ID].Lease, tryon.Image{}, "模型生成失败"); err != nil {
		t.Fatal(err)
	}
	if preview, err := repo.GarmentJobResult(ctx, owner, first[0].ID); err != nil || !bytes.Equal(preview.Data, source.Data) {
		t.Fatalf("未保存待确认生成图：%v", err)
	}

	type saveResult struct {
		asset tryon.Asset
		err   error
	}
	saves := make(chan saveResult, 2)
	for range 2 {
		go func() {
			asset, err := repo.SaveGarmentJob(ctx, owner, first[0].ID)
			saves <- saveResult{asset, err}
		}()
	}
	for range 2 {
		saved := <-saves
		if saved.err != nil || saved.asset.ID != first[0].ID || saved.asset.Kind != tryon.Garment || saved.asset.Name != items[0].Name {
			t.Fatalf("并发确认应只创建一件素材：%+v，错误：%v", saved.asset, saved.err)
		}
	}
	if asset, err := repo.Asset(ctx, owner, first[0].ID); err != nil || !bytes.Equal(asset.Data, source.Data) {
		t.Fatalf("确认后生成图没有入库：%v", err)
	}
	if err := repo.DeleteAsset(ctx, owner, first[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveGarmentJob(ctx, owner, first[0].ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("重复确认不能恢复已删除素材：%v", err)
	}

	expired, err := repo.ClaimGarmentJob(ctx, time.Now().Add(-time.Second))
	if err != nil || expired.Job.ID != added[0].ID {
		t.Fatalf("追加任务未被领取：%+v，错误：%v", expired.Job, err)
	}
	if _, err := repo.ClaimGarmentJob(ctx, time.Now().Add(time.Minute)); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("过期任务不能自动重领：%v", err)
	}
	retried, err := repo.RetryGarmentJob(ctx, owner, expired.Job.ID)
	if err != nil || retried.Status != "queued" || retried.Error != "" {
		t.Fatalf("过期任务应该支持显式重试：%+v，错误：%v", retried, err)
	}
	current, err := repo.ClaimGarmentJob(ctx, time.Now().Add(time.Minute))
	if err != nil || current.Job.ID != expired.Job.ID {
		t.Fatalf("重试任务未重新领取：%+v，错误：%v", current.Job, err)
	}
	if err := repo.FinishGarmentJob(ctx, expired.Job.ID, expired.Lease, source, ""); !errors.Is(err, tryon.ErrGarmentJobState) {
		t.Fatalf("旧领取不应覆盖正在运行的重试：%v", err)
	}

	// 模拟已保存的旧结果过期，同时保留较新的失败任务及仍在运行的任务。
	if _, err := pool.Exec(ctx, `UPDATE tryon_garment_jobs SET updated_at = NOW() - INTERVAL '8 days' WHERE id = $1`, first[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.PruneGarmentJobs(ctx, time.Now().Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveGarmentJob(ctx, owner, first[0].ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("到期任务应被清理：%v", err)
	}
	jobs, err := repo.ListGarmentJobs(ctx, owner)
	if err != nil || len(jobs) != 2 || jobs[0].Status != "running" || jobs[1].Status != "failed" {
		t.Fatalf("保留活动任务和近期失败：%+v，错误：%v", jobs, err)
	}
	if err := repo.FinishGarmentJob(ctx, current.Job.ID, current.Lease, source, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tryon_garment_jobs SET updated_at = NOW() - INTERVAL '8 days' WHERE id = $1`, current.Job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tryon_garment_batches SET created_at = NOW() - INTERVAL '8 days'
		WHERE id IN (SELECT batch_id FROM tryon_garment_jobs WHERE id = $1)`, current.Job.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.PruneGarmentJobs(ctx, time.Now().Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM tryon_garment_batches
		WHERE organization_id = $1 AND owner_id = $2`, owner.OrganizationID, owner.UserID).Scan(&batchCount)
	if err != nil || batchCount != 1 {
		t.Fatalf("过期任务的原图应在批次清空后删除，得到 %d 批，错误：%v", batchCount, err)
	}

	// 并行提交三个批次时，两批最多写入 16 件，超限批次不能部分入库。
	fullBatch := make([]tryon.GarmentJobItem, tryon.MaxGarmentBatch)
	for i := range fullBatch {
		fullBatch[i] = items[0]
	}
	submissions := make(chan error, 3)
	for range 3 {
		go func() {
			_, err := repo.CreateGarmentJobs(ctx, owner, model, fullBatch, source)
			submissions <- err
		}()
	}
	var accepted, limited int
	for range 3 {
		switch err := <-submissions; {
		case err == nil:
			accepted++
		case errors.Is(err, tryon.ErrGarmentJobLimit):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 2 || limited != 1 {
		t.Fatalf("并发限额不正确，接受 %d 批，拒绝 %d 批", accepted, limited)
	}
	if _, err := repo.RetryGarmentJob(ctx, owner, first[1].ID); !errors.Is(err, tryon.ErrGarmentJobLimit) {
		t.Fatalf("重试也应受同一活动任务限额约束：%v", err)
	}
}
