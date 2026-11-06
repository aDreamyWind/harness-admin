package tryon

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

type garmentQueueFixture struct {
	GarmentJobRepository
	queue    chan GarmentWork
	finished chan string
}

func (r *garmentQueueFixture) CreateGarmentJobs(_ context.Context, owner Owner, model string, items []GarmentJobItem, source Image) ([]GarmentJob, error) {
	jobs := make([]GarmentJob, 0, len(items))
	for _, item := range items {
		job := GarmentJob{ID: uuid.NewString(), Name: item.Name, Category: item.Category, ModelCode: model, Status: "queued"}
		jobs = append(jobs, job)
		r.queue <- GarmentWork{Job: job, Owner: owner, Box: item.Box, Source: source}
	}
	return jobs, nil
}

func (r *garmentQueueFixture) ClaimGarmentJob(_ context.Context, lease time.Time) (GarmentWork, error) {
	select {
	case work := <-r.queue:
		work.Lease = lease
		return work, nil
	default:
		return GarmentWork{}, ErrNotFound
	}
}

func (r *garmentQueueFixture) FinishGarmentJob(_ context.Context, id string, _ time.Time, _ Image, failure string) error {
	if failure != "" {
		return errors.New(failure)
	}
	r.finished <- id
	return nil
}

func (*garmentQueueFixture) PruneGarmentJobs(context.Context, time.Time) error { return nil }

type garmentEngineFixture struct {
	Engine
	started chan []Image
	release chan struct{}
}

func (*garmentEngineFixture) Models(context.Context, Owner) (Models, error) {
	return Models{Image: []Model{{Code: "editor"}}}, nil
}

func (e *garmentEngineFixture) Generate(ctx context.Context, _ Owner, _, _ string, _ string, refs []Image) (Image, error) {
	e.started <- refs
	select {
	case <-ctx.Done():
		return Image{}, ctx.Err()
	case <-e.release:
		return refs[len(refs)-1], nil
	}
}

func TestTryOnGarmentWorkerProcessesAdditionalBatches(t *testing.T) {
	repo := &garmentQueueFixture{queue: make(chan GarmentWork, 4), finished: make(chan string, 4)}
	engine := &garmentEngineFixture{started: make(chan []Image, 4), release: make(chan struct{}, 4)}
	service := NewService(nil, engine, slog.New(slog.NewTextHandler(io.Discard, nil))).WithGarmentJobs(repo)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.RunGarmentJobs(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 120, 120))); err != nil {
		t.Fatal(err)
	}
	owner := Owner{OrganizationID: "org", UserID: "user"}
	submit := func(items []GarmentJobItem) []GarmentJob {
		t.Helper()
		request, cancel := context.WithCancel(ctx)
		defer cancel()
		jobs, err := service.StartGarmentJobs(request, owner, "editor", items, source.Bytes())
		if err != nil || len(jobs) != len(items) {
			t.Fatalf("衣物未入队: %v，%d 件", err, len(jobs))
		}
		for _, job := range jobs {
			if job.Status != "queued" {
				t.Fatalf("应立即返回排队状态，得到 %s", job.Status)
			}
		}
		return jobs
	}
	first := submit([]GarmentJobItem{
		{Name: "白色针织衫", Category: "上衣", Box: &Box{0, 0, .5, .5}},
		{Name: "蓝色牛仔裤", Category: "裤子", Box: &Box{.5, .5, 1, 1}},
	})
	awaitReferences := func() {
		t.Helper()
		select {
		case refs := <-engine.started:
			if len(refs) != 2 || !bytes.Equal(refs[0].Data, source.Bytes()) || bytes.Equal(refs[0].Data, refs[1].Data) {
				t.Fatal("模型应收到原图和该任务的框选特写")
			}
		case <-ctx.Done():
			t.Fatal("未及时开始衣物生成")
		}
	}
	awaitReferences()
	awaitReferences()
	second := submit([]GarmentJobItem{{Name: "黑色手提包", Category: "包袋", Box: &Box{.2, .2, .7, .7}}})
	for range 3 {
		engine.release <- struct{}{}
	}
	awaitReferences()
	pending := map[string]bool{first[0].ID: true, first[1].ID: true, second[0].ID: true}
	for len(pending) != 0 {
		select {
		case id := <-repo.finished:
			delete(pending, id)
		case <-ctx.Done():
			t.Fatalf("提交请求结束后任务未全部完成：%v", pending)
		}
	}
}
