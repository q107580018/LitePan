package driver

import "context"

// OfflineDownloadCapabilities 描述驱动原生离线下载能力，供公共层动态探测。
type OfflineDownloadCapabilities struct {
	SupportsURLs       bool     `json:"supports_urls"`
	SupportsBatchURLs  bool     `json:"supports_batch_urls"`
	SupportsTorrent    bool     `json:"supports_torrent"`
	SupportsShareLinks bool     `json:"supports_share_links"`
	ShareLinkHosts     []string `json:"share_link_hosts,omitempty"`
	URLSchemes         []string `json:"url_schemes"`
	RootTargetAllowed  bool     `json:"root_target_allowed"`
	RemoteDelete       bool     `json:"remote_delete"`
}

// OfflineShareFile 是分享根目录下可选择转存的项目。
type OfflineShareFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	IsDir  bool   `json:"is_dir"`
	Wanted bool   `json:"wanted"`
}

type OfflineSharePrepareRequest struct {
	Link     string
	Passcode string
}

// OfflineSharePreparation 的 State 只在服务端内存中保存，不返回前端。
type OfflineSharePreparation struct {
	Source    string
	Name      string
	TotalSize int64
	Files     []OfflineShareFile
	State     any
}

type OfflineShareSaveRequest struct {
	Preparation OfflineSharePreparation
	FileIDs     []string
	ParentID    string
}

// OfflineShareResult 描述已转存到目标网盘的顶层内容。
type OfflineShareResult struct {
	Name           string
	Size           int64
	FileID         string
	ProviderTaskID string
	Completed      bool
	Message        string
}

// OfflineShareProvider 由支持分享链接预解析和转存的驱动实现。
type OfflineShareProvider interface {
	PrepareOfflineShare(ctx context.Context, req OfflineSharePrepareRequest) (*OfflineSharePreparation, error)
	SaveOfflineShare(ctx context.Context, req OfflineShareSaveRequest) (*OfflineShareResult, error)
}

// OfflineURLRequest 是链接离线下载的统一输入。
type OfflineURLRequest struct {
	URLs     []string
	ParentID string
	FileName string
}

// OfflineAddResult 是驱动创建一个远端任务后的结果。
type OfflineAddResult struct {
	Source         string
	ProviderTaskID string
	InfoHash       string
	Name           string
	Success        bool
	Message        string
}

// OfflineTaskRef 是公共层持久化后传给驱动刷新的最小引用。
type OfflineTaskRef struct {
	ProviderTaskID string
	InfoHash       string
}

// OfflineTaskUpdate 是驱动归一化后的远端任务状态。
type OfflineTaskUpdate struct {
	ProviderTaskID string
	InfoHash       string
	Status         string
	Progress       int
	Size           int64
	Name           string
	FileID         string
	Message        string
	Error          string
}

const (
	OfflineStatusPending  = "pending"
	OfflineStatusRunning  = "running"
	OfflineStatusRetrying = "retrying"
	OfflineStatusSuccess  = "success"
	OfflineStatusFailed   = "failed"
)

// OfflineDownloadProvider 声明驱动支持的原生离线下载能力。
type OfflineDownloadProvider interface {
	OfflineDownloadCapabilities() OfflineDownloadCapabilities
}

type OfflineURLDownloader interface {
	AddOfflineURLs(ctx context.Context, req OfflineURLRequest) ([]OfflineAddResult, error)
}

type OfflineTaskRefresher interface {
	RefreshOfflineTasks(ctx context.Context, refs []OfflineTaskRef) ([]OfflineTaskUpdate, error)
}

type OfflineTaskDeleter interface {
	DeleteOfflineTask(ctx context.Context, ref OfflineTaskRef, deleteSourceFile bool) error
}

// OfflineTorrentFile 是 BT 种子解析后的可选文件。
type OfflineTorrentFile struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Wanted bool   `json:"wanted"`
}

// OfflineTorrentPreparation 保存后端提交 BT 任务所需的临时信息。
type OfflineTorrentPreparation struct {
	TorrentName string
	TotalSize   int64
	InfoHash    string
	TorrentSHA1 string
	PickCode    string
	SeedFileID  string
	Files       []OfflineTorrentFile
}

type OfflineTorrentRequest struct {
	Preparation OfflineTorrentPreparation
	Wanted      []int
	ParentID    string
	SavePath    string
}

// OfflineTorrentDownloader 是支持 BT 种子的驱动额外实现的可选能力。
type OfflineTorrentDownloader interface {
	PrepareOfflineTorrent(ctx context.Context, localPath, fileName string) (*OfflineTorrentPreparation, error)
	AddOfflineTorrent(ctx context.Context, req OfflineTorrentRequest) (*OfflineAddResult, error)
}
