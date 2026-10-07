ALTER TABLE offline_download_tasks ADD COLUMN rename_pending INTEGER NOT NULL DEFAULT 0;
ALTER TABLE offline_download_tasks ADD COLUMN rename_target TEXT NOT NULL DEFAULT '';
ALTER TABLE offline_download_tasks ADD COLUMN rename_origin TEXT NOT NULL DEFAULT '';
