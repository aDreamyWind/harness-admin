package database

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"harness-admin/internal/store/postgres"
	"harness-admin/internal/tryon"
)

func testTryOnQueueAndOutfits(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	owner := tryon.Owner{OrganizationID: orgID}
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE organization_id = $1 AND username = 'admin'`, orgID).Scan(&owner.UserID); err != nil {
		t.Fatal(err)
	}
	stranger := tryon.Owner{OrganizationID: orgID, UserID: uuid.NewString()}
	repo := postgres.NewTryOnRepository(pool)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	photo := tryon.Image{Data: buf.Bytes(), MIME: "image/png"}
	asset := func(kind, name, category string) tryon.Asset {
		t.Helper()
		input := tryon.AssetImage{Asset: tryon.Asset{ID: uuid.NewString(), Kind: kind, Name: name, Category: category}, Image: photo}
		saved, err := repo.SaveAsset(ctx, owner, input)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	person := asset(tryon.PersonReal, "人物", "")
	jacket := asset(tryon.Garment, "外套", "外套")
	trousers := asset(tryon.Garment, "裤子", "裤子")
	skirt := asset(tryon.Garment, "套装试选半裙", "半裙")

	outfitIDs := []string{skirt.ID, jacket.ID}
	outfit, err := repo.CreateOutfit(ctx, owner, tryon.Outfit{ID: uuid.NewString(), Name: "通勤套装", GarmentIDs: outfitIDs}, nil)
	if err != nil || outfit.CreatedAt.IsZero() {
		t.Fatalf("保存套装失败：%+v，错误：%v", outfit, err)
	}
	if err := repo.DeleteAsset(ctx, owner, skirt.ID); err != nil {
		t.Fatalf("套装引用不应阻止删除衣物：%v", err)
	}
	if _, err := repo.Asset(ctx, owner, skirt.ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("仅由套装引用的衣物应完全删除：%v", err)
	}
	if got, err := repo.ListOutfits(ctx, owner); err != nil || len(got) != 1 || !slices.Equal(got[0].GarmentIDs, outfitIDs) {
		t.Fatalf("删除衣物后套装应保留有序引用：%+v，错误：%v", got, err)
	}
	if got, err := repo.ListOutfits(ctx, stranger); err != nil || len(got) != 0 {
		t.Fatalf("他人不可查看套装：%+v，错误：%v", got, err)
	}
	if err := repo.DeleteOutfit(ctx, stranger, outfit.ID); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("他人不可删除套装：%v", err)
	}
	if err := repo.DeleteOutfit(ctx, owner, outfit.ID); err != nil {
		t.Fatalf("本人应可删除套装：%v", err)
	}
	if got, err := repo.ListOutfits(ctx, owner); err != nil || len(got) != 0 {
		t.Fatalf("套装仍可见：%+v，错误：%v", got, err)
	}

	newJob := func(ids ...string) tryon.Job {
		return tryon.Job{ID: uuid.NewString(), PersonID: person.ID, GarmentIDs: ids, ModelCode: "editor"}
	}
	want := newJob(jacket.ID, trousers.ID)
	first, err := repo.CreateJob(ctx, owner, want, time.Time{}, nil)
	if err != nil || first.Status != "queued" || first.GarmentID != jacket.ID || !slices.Equal(first.GarmentIDs, want.GarmentIDs) {
		t.Fatalf("试穿任务入队未保留全部衣物：%+v，错误：%v", first, err)
	}
	second, err := repo.CreateJob(ctx, owner, newJob(jacket.ID), time.Time{}, nil)
	if err != nil || second.Status != "queued" {
		t.Fatalf("首个任务完成前应可追加：%+v，错误：%v", second, err)
	}
	if err := repo.DeleteAsset(ctx, owner, trousers.ID); err != nil {
		t.Fatalf("删除非首件衣物不应破坏队列引用：%v", err)
	}
	if got, err := repo.Asset(ctx, owner, trousers.ID); err != nil || !got.Deleted || !bytes.Equal(got.Data, photo.Data) {
		t.Fatalf("后台任务采用的非首件衣物应保留快照：%+v，错误：%v", got.Asset, err)
	}
	if _, err := repo.CreateJob(ctx, owner, newJob(jacket.ID, trousers.ID), time.Time{}, nil); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("删除的非首件衣物不可用于新任务：%v", err)
	}
	history, err := repo.ListJobs(ctx, owner)
	historyIndex := slices.IndexFunc(history, func(job tryon.Job) bool { return job.ID == first.ID })
	if err != nil || historyIndex < 0 || !slices.Equal(history[historyIndex].GarmentIDs, want.GarmentIDs) {
		t.Fatalf("列表未读回历史衣物顺序：%+v，错误：%v", history, err)
	}

	type claimResult struct {
		work tryon.TryOnWork
		err  error
	}
	claims := make(chan claimResult, 2)
	for range 2 {
		go func() {
			work, err := repo.ClaimTryOnJob(ctx, time.Now().Add(time.Minute))
			claims <- claimResult{work, err}
		}()
	}
	active := make(map[string]tryon.TryOnWork)
	for range 2 {
		claim := <-claims
		work := claim.work
		if claim.err != nil || work.Job.Status != "running" || work.Owner != owner || work.Lease.IsZero() {
			t.Fatalf("并发领取失败：%+v，错误：%v", work.Job, claim.err)
		}
		active[work.Job.ID] = work
	}
	if len(active) != 2 || !slices.Equal(active[first.ID].Job.GarmentIDs, want.GarmentIDs) || active[second.ID].Job.ID == "" {
		t.Fatalf("同一任务不应重复领取：%+v", active)
	}
	if err := repo.FinishTryOnJob(ctx, first.ID, active[first.ID].Lease.Add(-time.Second), photo, ""); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("错误领取令牌不能写入结果：%v", err)
	}
	if err := repo.FinishTryOnJob(ctx, first.ID, active[first.ID].Lease, photo, ""); err != nil {
		t.Fatalf("正确领取令牌应可完成试穿：%v", err)
	}
	if result, err := repo.Result(ctx, owner, first.ID); err != nil || !bytes.Equal(result.Data, photo.Data) {
		t.Fatalf("多件试穿结果未保存：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tryon_jobs SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimTryOnJob(ctx, time.Now().Add(time.Minute)); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("过期试穿不应自动重领：%v", err)
	}
	history, err = repo.ListJobs(ctx, owner)
	failedIndex := slices.IndexFunc(history, func(job tryon.Job) bool { return job.ID == second.ID && job.Status == "failed" && job.Error != "" })
	if err != nil || failedIndex < 0 {
		t.Fatalf("过期试穿没有明确失败：%+v，错误：%v", history, err)
	}
	if err := repo.FinishTryOnJob(ctx, second.ID, active[second.ID].Lease, photo, ""); !errors.Is(err, tryon.ErrNotFound) {
		t.Fatalf("过期任务不能接受迟到的结果：%v", err)
	}

	const submissions = tryon.MaxPendingTryOnJobs + 1
	done := make(chan error, submissions)
	for range submissions {
		go func() {
			_, err := repo.CreateJob(ctx, owner, newJob(jacket.ID), time.Time{}, nil)
			done <- err
		}()
	}
	var accepted, limited int
	for range submissions {
		switch err := <-done; {
		case err == nil:
			accepted++
		case errors.Is(err, tryon.ErrBusy):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if accepted != tryon.MaxPendingTryOnJobs || limited != 1 {
		t.Fatalf("同用户队列并发额度不正确：接受 %d 次，拒绝 %d 次", accepted, limited)
	}
}

func testTryOnLegacyMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseURL string) {
	t.Helper()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*config)
	defer db.Close()

	repo := postgres.NewTryOnRepository(pool)
	for range 2 {
		if _, err := repo.ClaimTryOnJob(ctx, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("降级前应有多条正在运行的任务：%v", err)
		}
	}
	var queued, running int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status = 'queued'), COUNT(*) FILTER (WHERE status = 'running')
		FROM tryon_jobs`).Scan(&queued, &running)
	if err != nil || queued == 0 || running != 2 {
		t.Fatalf("降级测试需要排队任务和两条运行任务，得到 %d、%d：%v", queued, running, err)
	}
	if err := goose.DownToContext(ctx, db, "migrations", 70); err != nil {
		t.Fatalf("有排队任务时应能安全恢复旧迁移：%v", err)
	}
	var interrupted int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tryon_jobs
		WHERE status = 'failed' AND error_message = '服务回滚导致任务中断，请重新试穿'`).Scan(&interrupted); err != nil || interrupted != queued+running {
		t.Fatalf("降级应保留中断任务及说明，得到 %d，预期 %d：%v", interrupted, queued+running, err)
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("重新迁移旧版单件记录失败：%v", err)
	}
	const legacyJobID = "bb4ed136-72cd-4589-8ebd-6b8841edb776"
	var garmentID, associatedID, status string
	var position int
	err = pool.QueryRow(ctx, `SELECT j.garment_id, g.garment_id, j.status, g.position
		FROM tryon_jobs j JOIN tryon_job_garments g ON g.job_id = j.id WHERE j.id = $1`,
		legacyJobID).Scan(&garmentID, &associatedID, &status, &position)
	if err != nil || garmentID != associatedID || status != "succeeded" || position != 1 {
		t.Fatalf("旧单件结果应回填原衣物为第一件：衣物 %s，关联 %s，状态 %s，顺序 %d，错误 %v",
			garmentID, associatedID, status, position, err)
	}
}
