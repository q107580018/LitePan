package offlinedownload

import (
	"context"
	"sync/atomic"
	"testing"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

type offlineShareTestDriver struct {
	offlineTestDriver
	prepareRequest driver.OfflineSharePrepareRequest
	saveRequest    driver.OfflineShareSaveRequest
}

func (*offlineShareTestDriver) OfflineDownloadCapabilities() driver.OfflineDownloadCapabilities {
	return driver.OfflineDownloadCapabilities{SupportsShareLinks: true, ShareLinkHosts: []string{"share.example.com"}}
}

func (d *offlineShareTestDriver) PrepareOfflineShare(_ context.Context, req driver.OfflineSharePrepareRequest) (*driver.OfflineSharePreparation, error) {
	d.prepareRequest = req
	return &driver.OfflineSharePreparation{
		Name: "电影合集", TotalSize: 3072, State: "provider-secret",
		Files: []driver.OfflineShareFile{
			{ID: "file-1", Name: "电影.mkv", Size: 1024, Wanted: true},
			{ID: "dir-1", Name: "花絮", Size: 2048, IsDir: true, Wanted: true},
		},
	}, nil
}

func (d *offlineShareTestDriver) SaveOfflineShare(_ context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	d.saveRequest = req
	return &driver.OfflineShareResult{Name: "电影.mkv", Size: 1024, Completed: true, Message: "转存完成"}, nil
}

func TestPrepareAndAddShareCreatesCompletedTask(t *testing.T) {
	drv := &offlineShareTestDriver{}
	repo := newOfflineTaskRepo()
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "share-test"}},
		Repo:     repo,
	})

	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{
		AccountID: 7, Link: "资源 https://share.example.com/s/abc 提取码：1234", Passcode: "1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparation.PreparationID == "" || len(preparation.Files) != 2 || preparation.TotalSize != 3072 {
		t.Fatalf("分享解析结果不正确: %+v", preparation)
	}
	if drv.prepareRequest.Passcode != "1234" {
		t.Fatalf("解析请求未传递提取码: %+v", drv.prepareRequest)
	}

	task, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
		TargetParentID: "folder-1", TargetDisplayPath: "/影视",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.SourceKind != SourceShare || task.Status != driver.OfflineStatusSuccess || task.Progress != 100 {
		t.Fatalf("转存任务状态不正确: %+v", task)
	}
	if task.Name != "电影.mkv" || task.Size != 1024 || task.TargetParentID != "folder-1" {
		t.Fatalf("转存任务内容不正确: %+v", task)
	}
	if task.Source != "https://share.example.com/s/abc" {
		t.Fatalf("任务来源应移除分享文案和提取码，实际 %q", task.Source)
	}
	if len(drv.saveRequest.FileIDs) != 1 || drv.saveRequest.FileIDs[0] != "file-1" || drv.saveRequest.ParentID != "folder-1" {
		t.Fatalf("驱动转存请求不正确: %+v", drv.saveRequest)
	}
	if drv.saveRequest.Preparation.State != "provider-secret" {
		t.Fatal("provider 私有状态未在后端 preparation 中保留")
	}
	if _, ok := repo.tasks[task.TaskID]; !ok {
		t.Fatal("转存任务没有持久化")
	}
}

func TestAddShareRejectsEmptySelection(t *testing.T) {
	drv := &offlineShareTestDriver{}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "share-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{AccountID: 7, Link: "https://share.example.com/s/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddShare(context.Background(), AddShareParams{AccountID: 7, PreparationID: preparation.PreparationID}); err == nil {
		t.Fatal("空选择应返回错误")
	}
	if len(drv.saveRequest.FileIDs) != 0 {
		t.Fatalf("空选择不应调用驱动，实际 %v", drv.saveRequest.FileIDs)
	}
}

type offlineAnyAccountRepo struct{ offlineAccountRepo }

func (offlineAnyAccountRepo) Get(_ context.Context, id int64) (*domain.Account, error) {
	return &domain.Account{ID: id, Name: "测试盘", DriverType: "share-test"}, nil
}

func TestSharePreparationIsBoundToAccount(t *testing.T) {
	drv := &offlineShareTestDriver{}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAnyAccountRepo{},
		Repo:     newOfflineTaskRepo(),
	})
	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{AccountID: 7, Link: "https://share.example.com/s/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 8, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
	}); err == nil {
		t.Fatal("其他账号不应消费 preparation")
	}
	if _, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
	}); err != nil {
		t.Fatalf("其他账号的请求不应破坏原 preparation: %v", err)
	}
}

type offlineAsyncShareDriver struct {
	offlineShareTestDriver
}

func (d *offlineAsyncShareDriver) SaveOfflineShare(_ context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	d.saveRequest = req
	return &driver.OfflineShareResult{Name: "电影.mkv", Size: 1024, ProviderTaskID: "share-task-1", Message: "已提交"}, nil
}

func TestAsyncShareTaskIsRefreshedToSuccess(t *testing.T) {
	drv := &offlineAsyncShareDriver{}
	drv.updates = []driver.OfflineTaskUpdate{{
		ProviderTaskID: "share-task-1", Status: driver.OfflineStatusSuccess, Progress: 100, Message: "转存完成",
	}}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "share-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{AccountID: 7, Link: "https://share.example.com/s/abc"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != driver.OfflineStatusPending || task.ProviderTaskID != "share-task-1" {
		t.Fatalf("异步转存任务创建状态不正确: %+v", task)
	}
	if err := svc.Refresh(context.Background(), 7, true); err != nil {
		t.Fatal(err)
	}
	tasks, err := svc.List(context.Background(), 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != driver.OfflineStatusSuccess || tasks[0].Phase != PhaseDone {
		t.Fatalf("异步转存任务未更新为成功: %+v", tasks)
	}
}

type offlineBlockingShareDriver struct {
	offlineShareTestDriver
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (d *offlineBlockingShareDriver) SaveOfflineShare(_ context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	d.saveRequest = req
	d.calls.Add(1)
	d.started <- struct{}{}
	<-d.release
	return &driver.OfflineShareResult{Name: "电影.mkv", Size: 1024, Completed: true}, nil
}

func TestAddShareConsumesPreparationAtomically(t *testing.T) {
	drv := &offlineBlockingShareDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "share-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{AccountID: 7, Link: "https://share.example.com/s/abc"})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, addErr := svc.AddShare(context.Background(), AddShareParams{
			AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
		})
		firstDone <- addErr
	}()
	<-drv.started
	if _, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
	}); err == nil {
		t.Fatal("同一个 preparation 不应并发提交两次")
	}
	close(drv.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if got := drv.calls.Load(); got != 1 {
		t.Fatalf("驱动调用次数 = %d, want 1", got)
	}
}

func TestPrepareShareRejectsUnsupportedDriver(t *testing.T) {
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: &offlineTestDriver{}}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "offline-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	if _, err := svc.PrepareShare(context.Background(), PrepareShareParams{AccountID: 7, Link: "https://share.example.com/s/abc"}); err == nil {
		t.Fatal("不支持分享转存的驱动应返回错误")
	}
}
