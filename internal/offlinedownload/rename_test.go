package offlinedownload

import (
	"context"
	"testing"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

// 覆盖「转存完成后确认重命名」：转存时保留网盘原始名称，用户在任务完成后确认才改名。

type offlineRenameCall struct {
	fileID  string
	newName string
}

type offlineRenameTestDriver struct {
	offlineShareTestDriver
	result      *driver.OfflineShareResult
	renameCalls []offlineRenameCall
	listItems   []domain.FileItem
}

func (d *offlineRenameTestDriver) SaveOfflineShare(_ context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	d.saveRequest = req
	copy := *d.result
	return &copy, nil
}

func (d *offlineRenameTestDriver) RenameFile(_ context.Context, fileID, newName string) error {
	d.renameCalls = append(d.renameCalls, offlineRenameCall{fileID: fileID, newName: newName})
	return nil
}

func (d *offlineRenameTestDriver) ListFiles(_ context.Context, _ string) ([]domain.FileItem, error) {
	return append([]domain.FileItem(nil), d.listItems...), nil
}

func newOfflineRenameTestDriver(result *driver.OfflineShareResult) *offlineRenameTestDriver {
	return &offlineRenameTestDriver{result: result}
}

func newOfflineRenameService(t *testing.T, drv *offlineRenameTestDriver) (*Service, *offlineTaskRepo) {
	t.Helper()
	repo := newOfflineTaskRepo()
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "share-test"}},
		Repo:     repo,
	})
	return svc, repo
}

func addDeferredShare(t *testing.T, svc *Service, confirm bool) *Task {
	t.Helper()
	preparation, err := svc.PrepareShare(context.Background(), PrepareShareParams{
		AccountID: 7, Link: "https://share.example.com/s/abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.AddShare(context.Background(), AddShareParams{
		AccountID: 7, PreparationID: preparation.PreparationID, FileIDs: []string{"file-1"},
		TargetParentID: "folder-1", TargetDisplayPath: "/影视",
		TargetName: "PanSou 标题", RenameConfirm: confirm,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestAddShareDefersRenameUntilConfirmed(t *testing.T) {
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{
		Name: "电影.mkv", Size: 1024, Completed: true, FileID: "file-9", RenameDeferred: true,
	})
	svc, repo := newOfflineRenameService(t, drv)

	task := addDeferredShare(t, svc, true)
	// 目标名不下发驱动，只告知驱动需要延迟重命名。
	if drv.saveRequest.TargetName != "" || !drv.saveRequest.DeferRename {
		t.Fatalf("延迟重命名时驱动不应拿到目标名: %+v", drv.saveRequest)
	}
	if !task.RenamePending || task.RenameTarget != "PanSou 标题" || task.RenameOrigin != "电影.mkv" {
		t.Fatalf("任务未携带待确认的重命名: %+v", task)
	}
	if task.Name != "电影.mkv" {
		t.Fatalf("确认前应保留网盘原始名称，实际 %q", task.Name)
	}
	rec, ok := repo.tasks[task.TaskID]
	if !ok || !rec.RenamePending || rec.RenameTarget != "PanSou 标题" || rec.RenameOrigin != "电影.mkv" {
		t.Fatalf("待确认状态未持久化: %+v", rec)
	}

	resolved, err := svc.ConfirmRename(context.Background(), task.TaskID, false)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RenamePending || resolved.Name != "电影.mkv" {
		t.Fatalf("放弃重命名后应保留原名: %+v", resolved)
	}
	if len(drv.renameCalls) != 0 {
		t.Fatalf("放弃重命名不应调用驱动改名: %+v", drv.renameCalls)
	}
	if _, err := svc.ConfirmRename(context.Background(), task.TaskID, false); err == nil {
		t.Fatal("已确认过的任务再次确认应报错")
	}
}

func TestConfirmRenameUsesDriverFileID(t *testing.T) {
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{
		Name: "电影.mkv", Size: 1024, Completed: true, FileID: "file-9", RenameDeferred: true,
	})
	svc, _ := newOfflineRenameService(t, drv)
	task := addDeferredShare(t, svc, true)

	resolved, err := svc.ConfirmRename(context.Background(), task.TaskID, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RenamePending || resolved.RenameTarget != "" {
		t.Fatalf("确认后应清空待确认状态: %+v", resolved)
	}
	if resolved.Name != "PanSou 标题" {
		t.Fatalf("确认后任务名应更新为目标名，实际 %q", resolved.Name)
	}
	if len(drv.renameCalls) != 1 || drv.renameCalls[0].fileID != "file-9" || drv.renameCalls[0].newName != "PanSou 标题" {
		t.Fatalf("改名调用不正确: %+v", drv.renameCalls)
	}
}

func TestConfirmRenameFallsBackToDirectoryLookup(t *testing.T) {
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{
		Name: "电影.mkv", Size: 1024, Completed: true, RenameDeferred: true,
	})
	drv.listItems = []domain.FileItem{
		{ID: "other-1", Name: "别的文件.mkv"},
		{ID: "new-1", Name: "电影.mkv"},
	}
	svc, _ := newOfflineRenameService(t, drv)
	task := addDeferredShare(t, svc, true)

	if _, err := svc.ConfirmRename(context.Background(), task.TaskID, true); err != nil {
		t.Fatal(err)
	}
	if len(drv.renameCalls) != 1 || drv.renameCalls[0].fileID != "new-1" {
		t.Fatalf("驱动未回填 file_id 时应按原始名称查找: %+v", drv.renameCalls)
	}
}

func TestConfirmRenameUnknownTask(t *testing.T) {
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{Name: "电影.mkv", Completed: true})
	svc, _ := newOfflineRenameService(t, drv)
	if _, err := svc.ConfirmRename(context.Background(), "missing", true); err == nil {
		t.Fatal("未知任务应报错")
	}
}

func TestAddShareWithoutConfirmKeepsImmediateRename(t *testing.T) {
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{Name: "PanSou 标题", Completed: true})
	svc, _ := newOfflineRenameService(t, drv)

	task := addDeferredShare(t, svc, false)
	if drv.saveRequest.TargetName != "PanSou 标题" || drv.saveRequest.DeferRename {
		t.Fatalf("未开启确认时应直接下发目标名: %+v", drv.saveRequest)
	}
	if task.RenamePending || task.RenameTarget != "" {
		t.Fatalf("未开启确认时不应有待确认状态: %+v", task)
	}
}

type offlineRenameURLDriver struct {
	offlineTestDriver
	renameCalls []offlineRenameCall
	fileName    string
	listItems   []domain.FileItem
}

func (d *offlineRenameURLDriver) OfflineDownloadCapabilities() driver.OfflineDownloadCapabilities {
	return driver.OfflineDownloadCapabilities{SupportsURLs: true, URLSchemes: []string{"https", "magnet"}}
}

func (d *offlineRenameURLDriver) AddOfflineURLs(_ context.Context, req driver.OfflineURLRequest) ([]driver.OfflineAddResult, error) {
	d.fileName = req.FileName
	return []driver.OfflineAddResult{{Source: req.URLs[0], InfoHash: "hash-1", Success: true}}, nil
}

func (d *offlineRenameURLDriver) RenameFile(_ context.Context, fileID, newName string) error {
	d.renameCalls = append(d.renameCalls, offlineRenameCall{fileID: fileID, newName: newName})
	return nil
}

func (d *offlineRenameURLDriver) ListFiles(_ context.Context, _ string) ([]domain.FileItem, error) {
	return append([]domain.FileItem(nil), d.listItems...), nil
}

func TestAddURLsDefersRenameUntilConfirmed(t *testing.T) {
	drv := &offlineRenameURLDriver{listItems: []domain.FileItem{{ID: "new-1", Name: "电影.mkv"}}}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "offline-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	tasks, err := svc.AddURLs(context.Background(), AddURLParams{
		AccountID: 7, URLs: []string{"magnet:?xt=urn:btih:abc&dn=电影.mkv"}, FileName: "PanSou 标题",
		TargetParentID: "folder-1", TargetDisplayPath: "/影视", RenameConfirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应创建 1 个任务，实际 %d", len(tasks))
	}
	task := tasks[0]
	if drv.fileName != "" {
		t.Fatalf("延迟重命名时不应下发文件名，实际 %q", drv.fileName)
	}
	if !task.RenamePending || task.RenameTarget != "PanSou 标题" {
		t.Fatalf("任务未携带待确认的重命名: %+v", task)
	}

	// URL 离线下载是异步任务：完成后驱动会把任务名刷新为网盘真实名称，再确认重命名。
	refreshed := task
	refreshed.Status = driver.OfflineStatusSuccess
	refreshed.Name = "电影.mkv"
	svc.putTask(&refreshed)

	resolved, err := svc.ConfirmRename(context.Background(), task.TaskID, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != "PanSou 标题" || resolved.RenamePending {
		t.Fatalf("确认后应改名并清空状态: %+v", resolved)
	}
	if len(drv.renameCalls) != 1 || drv.renameCalls[0].fileID != "new-1" {
		t.Fatalf("改名调用不正确: %+v", drv.renameCalls)
	}
}

func TestRefreshClearsPendingRenameOnFailure(t *testing.T) {
	// 异步分享转存：保存时已经确定要延迟重命名，但转存任务随后失败。
	drv := newOfflineRenameTestDriver(&driver.OfflineShareResult{
		Name: "电影.mkv", ProviderTaskID: "share-task-1", RenameDeferred: true, Message: "已提交",
	})
	drv.updates = []driver.OfflineTaskUpdate{{
		ProviderTaskID: "share-task-1", Status: driver.OfflineStatusFailed, Error: "转存失败",
	}}
	svc, _ := newOfflineRenameService(t, drv)
	task := addDeferredShare(t, svc, true)
	if !task.RenamePending || task.Status != driver.OfflineStatusPending {
		t.Fatalf("异步延迟重命名任务状态不正确: %+v", task)
	}

	if err := svc.Refresh(context.Background(), 7, true); err != nil {
		t.Fatal(err)
	}
	tasks, err := svc.List(context.Background(), 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != driver.OfflineStatusFailed {
		t.Fatalf("任务未刷新为失败: %+v", tasks)
	}
	if tasks[0].RenamePending || tasks[0].RenameTarget != "" {
		t.Fatalf("转存失败后不应保留待确认重命名: %+v", tasks[0])
	}
}

func TestAddURLsRenameConfirmWithoutTargetNameSkipsDeferral(t *testing.T) {
	// 没有目标名就没有可确认的重命名，任务不应挂上待确认状态。
	drv := &offlineRenameURLDriver{}
	svc := New(Options{
		Exec:     driverexec.New(offlineTestProvider{drv: drv}, nil),
		Accounts: offlineAccountRepo{account: &domain.Account{ID: 7, Name: "测试盘", DriverType: "offline-test"}},
		Repo:     newOfflineTaskRepo(),
	})
	tasks, err := svc.AddURLs(context.Background(), AddURLParams{
		AccountID: 7, URLs: []string{"magnet:?xt=urn:btih:abc"}, RenameConfirm: true,
		TargetParentID: "folder-1", TargetDisplayPath: "/影视",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].RenamePending {
		t.Fatalf("缺少目标名时不应有待确认重命名: %+v", tasks)
	}
}
