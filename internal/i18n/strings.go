package i18n

// Stable string IDs. Add new IDs here and to both tables.
const (
	idUpstreamBase    = "upstream_base"
	idListenAddr      = "listen_addr"
	idConnectTimeout  = "connect_timeout"
	idIdleTimeout     = "idle_timeout"
	idLogLevel        = "log_level"
	idSkipTLS         = "skip_tls"
	idAutostart       = "autostart"
	idLanguage        = "language"
	idThemeMode       = "theme_mode"
	idThemeLight      = "theme_light"
	idThemeDark       = "theme_dark"
	idConnSection     = "conn_section"
	idAppearanceSec   = "appearance_section"
	idLaunchSection   = "launch_section"
	idStart           = "start"
	idStop            = "stop"
	idSaveApply       = "save_apply"
	idRevert          = "revert"
	idRunning         = "running"
	idStopped         = "stopped"
	idErrorPrefix     = "error_prefix"
	idSettings        = "settings"
	idLogs            = "logs"
	idAbout           = "about"
	idConnLogs        = "conn_logs"
	idClear           = "clear"
	idFilter          = "filter"
	idCheckUpdate     = "check_update"
	idDownloadApply   = "download_apply"
	idCurrentVersion  = "current_version"
	idUpdateAvailable = "update_available"
	idNoUpdate        = "no_update"
	idUpdateFailed    = "update_failed"
	idUpdatedRestart  = "updated_restart"
	idInvalid         = "invalid"
)

var zh = map[string]string{
	idUpstreamBase:    "上游地址",
	idListenAddr:      "监听地址",
	idConnectTimeout:  "连接超时",
	idIdleTimeout:     "空闲超时",
	idLogLevel:        "日志级别",
	idSkipTLS:         "跳过上游 TLS 校验（仅调试）",
	idAutostart:       "开机自启",
	idLanguage:        "语言",
	idThemeMode:       "主题",
	idThemeLight:      "浅色",
	idThemeDark:       "深色",
	idConnSection:     "连接配置",
	idAppearanceSec:   "外观",
	idLaunchSection:   "启动",
	idStart:           "启动",
	idStop:            "停止",
	idSaveApply:       "保存并应用",
	idRevert:          "恢复",
	idRunning:         "运行中",
	idStopped:         "已停止",
	idErrorPrefix:     "错误",
	idSettings:        "设置",
	idLogs:            "日志",
	idAbout:           "关于",
	idConnLogs:        "连接日志",
	idClear:           "清空",
	idFilter:          "过滤",
	idCheckUpdate:     "检查更新",
	idDownloadApply:   "下载并应用更新",
	idCurrentVersion:  "当前版本",
	idUpdateAvailable: "有新版本",
	idNoUpdate:        "已是最新",
	idUpdateFailed:    "更新失败",
	idUpdatedRestart:  "已更新，请重启",
	idInvalid:         "无效",
}

var en = map[string]string{
	idUpstreamBase:    "Upstream base",
	idListenAddr:      "Listen address",
	idConnectTimeout:  "Connect timeout",
	idIdleTimeout:     "Idle timeout",
	idLogLevel:        "Log level",
	idSkipTLS:         "Skip upstream TLS verify (debug only)",
	idAutostart:       "Launch at login",
	idLanguage:        "Language",
	idThemeMode:       "Theme",
	idThemeLight:      "Light",
	idThemeDark:       "Dark",
	idConnSection:     "Connection",
	idAppearanceSec:   "Appearance",
	idLaunchSection:   "Startup",
	idStart:           "Start",
	idStop:            "Stop",
	idSaveApply:       "Save & Apply",
	idRevert:          "Revert",
	idRunning:         "Running",
	idStopped:         "Stopped",
	idErrorPrefix:     "Error",
	idSettings:        "Settings",
	idLogs:            "Logs",
	idAbout:           "About",
	idConnLogs:        "Connection logs",
	idClear:           "Clear",
	idFilter:          "Filter",
	idCheckUpdate:     "Check for updates",
	idDownloadApply:   "Download & apply update",
	idCurrentVersion:  "Current version",
	idUpdateAvailable: "Update available",
	idNoUpdate:        "Up to date",
	idUpdateFailed:    "Update failed",
	idUpdatedRestart:  "Updated — please restart",
	idInvalid:         "Invalid",
}
