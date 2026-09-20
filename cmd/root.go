// Package cmd 是命令入口。cmd 只解析参数与装配依赖，业务逻辑下沉到 internal/。
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ma6254/news-glean/internal/app"
	"github.com/ma6254/news-glean/internal/build"
	"github.com/ma6254/news-glean/internal/config"
	"github.com/ma6254/news-glean/internal/database"
	"github.com/ma6254/news-glean/internal/scheduler"
	"github.com/ma6254/news-glean/internal/server"
	"github.com/ma6254/news-glean/log"

	// 注册渠道实现（导入即触发 init 注册到 source 注册表）
	_ "github.com/ma6254/news-glean/internal/source/feed"
	_ "github.com/ma6254/news-glean/internal/source/webpage"

	"github.com/spf13/cobra"
)

var (
	cfgPath string // 配置文件路径
	workDir string // 工作目录
)

// rootCmd 是根命令，等价于启动常驻服务（serve）。
var rootCmd = &cobra.Command{
	Use:   "news-glean",
	Short: "拾取、清洗、归档你关心的内容",
	Long: `NewsGlean 把 RSS、网页、聊天机器人里的信息统一成同一个阅读流。
默认启动常驻服务，提供 /api 与 Web 界面。`,
	RunE:         runServe,
	SilenceUsage: true, // 出错时不刷 usage，只打印 Error 行
}

// versionCmd 打印版本信息。
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "打印版本信息",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("news-glean %s (build %s)\n", build.BuildVersion, build.BuildTime)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "配置文件路径（默认 ./config.yml）")
	rootCmd.PersistentFlags().StringVarP(&workDir, "dir", "d", "", "工作目录，启动前切换")
	rootCmd.AddCommand(versionCmd)
}

// runServe 装配依赖并启动服务，直到收到退出信号。
func runServe(cmd *cobra.Command, _ []string) error {
	// 切换工作目录，此后相对路径（数据库、导出目录）都基于它解析
	if workDir != "" {
		if err := os.Chdir(workDir); err != nil {
			return fmt.Errorf("chdir: %w", err)
		}
	}
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 首次运行无配置文件，使用内置默认值
			cfg = config.Default()
		} else {
			return fmt.Errorf("load config: %w", err)
		}
	}

	// 装配日志库：把应用日志配置映射到 log 库并应用到默认核心，
	// 之后的 fmt 输出与 log.Info 都会进入同一套终端/文件输出端。
	cleanupLog, err := setupLog(cfg.Log)
	if err != nil {
		return fmt.Errorf("setup log: %w", err)
	}
	defer cleanupLog()

	// 启动 banner：原始输出，不带时间/级别前缀，只进终端。
	log.Banner(banner())

	driver := cfg.Database.Driver
	if driver == "" {
		driver = "sqlite"
	}
	dsn := cfg.Database.SQLite.File
	if dsn == "" {
		dsn = "./data.db"
	}

	db, err := database.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if err := db.Install(); err != nil {
		return fmt.Errorf("install database: %w", err)
	}

	a := app.New(cfg, db)
	if n, err := a.SeedDefaults(); err != nil {
		return fmt.Errorf("seed default sources: %w", err)
	} else if n > 0 {
		log.Info("seeded default sources", "count", n)
	}
	sched := scheduler.New(cfg, db, a)
	srv := server.New(cfg, db, a, sched)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 挂上服务即启动定时后台调度：自动按渠道 interval 刷新，无需手动触发。
	sched.Start(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	log.Info("news-glean serving", "addr", "http://"+cfg.Server.HTTPAddr)
	log.Info("swagger UI available", "url", "http://"+cfg.Server.HTTPAddr+"/swagger/index.html")

	select {
	case err := <-errCh:
		sched.Stop()
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		// 先停调度（等待在途采集收敛），再优雅关闭 HTTP 服务。
		sched.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// 信号触发的关停是正常退出，不算错误；超时只记告警，不返回非零错误。
		if err := srv.Stop(shutdownCtx); err != nil {
			log.Warn("graceful shutdown timed out", "error", err)
		}
		return nil
	}
}

// setupLog 把应用日志配置映射到 log 库并应用到默认核心，返回关闭函数。
// 目录非空时按 access/error 双文件落盘（日志库自管大小轮转）；
// max_size/max_backups 映射到日志库的 MB 轮转参数；max_age 暂未接入（保留字段）。
func setupLog(lc config.LogConfig) (func(), error) {
	logCfg := log.DefaultConfig()
	logCfg.LogLevel = lc.Level
	if lc.MaxSize > 0 {
		logCfg.MaxSizeMB = lc.MaxSize
	}
	if lc.MaxBackups > 0 {
		logCfg.MaxBackups = lc.MaxBackups
	}
	if lc.Dir != "" {
		logCfg.Access = filepath.Join(lc.Dir, "access.log")
		logCfg.Error = filepath.Join(lc.Dir, "error.log")
	}
	if err := logCfg.Apply(log.DefaultCore()); err != nil {
		return nil, err
	}
	return func() { log.DefaultCore().Close() }, nil
}

// Execute 是程序入口，由 main 调用。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
