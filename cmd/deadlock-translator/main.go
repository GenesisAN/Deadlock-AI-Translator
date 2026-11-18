package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/localcert"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/server"
	"deadlocktranslator/internal/settings"
	"deadlocktranslator/internal/translate"
)

// Set by scripts/build_release.py for GitHub releases.
var githubRepository, buildVersion, buildCommit string

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
func run() error {
	state, err := stateDirectory()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	legacy := filepath.Join(filepath.Dir(exe), "config")
	if _, err = os.Stat(filepath.Join(legacy, "config.json")); errors.Is(err, os.ErrNotExist) {
		if _, err = os.Stat("config/config.json"); err == nil {
			legacy = "config"
		}
	}
	if legacyEnv := os.Getenv("BABEL_TOWER_CONFIG_DIR"); legacyEnv != "" {
		legacy = legacyEnv
	}
	var initTLS, checkTLS, noAutoCert, showVersion, collectSamples bool
	var certPath, certKey string
	var gamePort int
	flag.StringVar(&state, "state", state, "本机配置和凭据目录")
	flag.StringVar(&legacy, "legacy-config", legacy, "旧版 config 目录")
	flag.IntVar(&gamePort, "game-port", 8792, "游戏 HTTPS 端口，随附 VPK 使用 8792")
	flag.StringVar(&certPath, "tls-cert", "", "外部 PEM 服务器证书")
	flag.StringVar(&certKey, "tls-key", "", "外部 PEM 服务器私钥")
	flag.BoolVar(&initTLS, "init-tls", false, "生成或续期本机证书并输出根证书路径")
	flag.BoolVar(&checkTLS, "check-tls", false, "检查本机证书并输出根证书路径")
	flag.BoolVar(&noAutoCert, "no-auto-cert", false, "使用现有证书，跳过 Windows 自动证书设置")
	flag.BoolVar(&showVersion, "version", false, "显示程序版本和文件 SHA-256")
	flag.BoolVar(&collectSamples, "collect-samples", true, "在本机保存游戏内 AI 翻译样本")
	flag.Parse()
	certs := filepath.Join(state, "cert-go")
	if initTLS || checkTLS {
		if initTLS {
			if err = localcert.Init(certs); err != nil {
				return err
			}
		}
		if _, err = localcert.Load(certs); err != nil {
			return err
		}
		fmt.Println(localcert.RootPath(certs))
		return nil
	}
	version := buildVersion
	if version == "" {
		versionFile, err := assets.Files.ReadFile("VERSION")
		if err != nil {
			return err
		}
		version = strings.TrimSpace(string(versionFile))
	}
	infoErr := printBuildInfo(exe, version)
	if showVersion {
		return infoErr
	}
	if infoErr != nil {
		slog.Warn("无法读取程序 SHA-256", "error", infoErr)
	}
	if (certPath == "") != (certKey == "") {
		return errors.New("tls-cert 和 tls-key 需要同时指定")
	}
	store, err := settings.Open(state, filepath.Join(legacy, "config.json"))
	if err != nil {
		return err
	}
	defer store.Close()
	book, err := translate.NewProjectLexicon(assets.Files, state, legacy)
	if err != nil {
		return err
	}
	client := provider.NewClient()
	defer client.Close()
	updates, err := server.NewUpdates(githubRepository, version, exe)
	if err != nil {
		return err
	}
	engine := translate.New(client, book)
	if collectSamples {
		engine.SetSampleRecorder(translate.NewSampleRecorder(state))
		slog.Info("已启用本机 AI 翻译样本记录", "directory", filepath.Join(state, "translation-samples"))
	}
	localGlossaries := translate.NewLocalGlossaries(assets.Files, state, legacy, engine)
	if err := localGlossaries.MigrateContext(store); err != nil {
		return fmt.Errorf("迁移自定义词库失败: %w", err)
	}
	s, err := server.New(store, engine, assets.Files, version+"-go", updates, localGlossaries)
	if err != nil {
		return err
	}
	adminPort := store.Snapshot().Config.Port
	if gamePort < 1024 || gamePort > 65535 || gamePort == adminPort {
		return errors.New("游戏 HTTPS 端口无效或与设置页冲突")
	}
	type endpoint struct {
		http     *http.Server
		listener net.Listener
	}
	var servers []endpoint
	listen := func(port int, game bool, cert *tls.Certificate) error {
		v4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return err
		}
		listeners := []net.Listener{v4}
		if v6, e := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port)); e == nil {
			listeners = append(listeners, v6)
		}
		for _, listener := range listeners {
			h := &http.Server{Handler: s.Handler(game), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
			if cert != nil {
				h.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{*cert}}
			}
			servers = append(servers, endpoint{h, listener})
		}
		return nil
	}
	if err = listen(adminPort, false, nil); err != nil {
		return fmt.Errorf("设置页监听失败: %w", err)
	}
	var cert tls.Certificate
	var installedRoot bool
	if certPath != "" {
		cert, err = tls.LoadX509KeyPair(certPath, certKey)
	} else if noAutoCert {
		cert, err = localcert.Load(certs)
	} else {
		cert, installedRoot, err = localcert.Prepare(certs)
	}
	if installedRoot {
		slog.Info("本机根证书已受信，请完全退出 Steam（包括托盘）再重新启动 Steam")
	}
	if err == nil {
		err = listen(gamePort, true, &cert)
	}
	if err == nil {
		s.TLSReady(true)
		slog.Info("游戏桥正在监听", "url", fmt.Sprintf("https://localhost:%d/community-bridge", gamePort))
	} else {
		slog.Warn("游戏 HTTPS 入口不可用，设置页仍可使用", "reason", err)
	}
	slog.Info("设置页已启动", "url", fmt.Sprintf("http://127.0.0.1:%d/ui", adminPort))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for _, e := range servers {
		go func() {
			var err error
			if e.http.TLSConfig != nil {
				err = e.http.ServeTLS(e.listener, "", "")
			} else {
				err = e.http.Serve(e.listener)
			}
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("监听退出", "address", e.listener.Addr(), "error", err)
			}
		}()
	}
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, e := range servers {
		if err = e.http.Shutdown(shutdown); err != nil {
			_ = e.http.Close()
		}
	}
	return nil
}

func printBuildInfo(executable, version string) error {
	fmt.Printf("Deadlock AI Translator %s-go\n", version)
	if buildCommit != "" {
		fmt.Printf("Git commit: %s\n", buildCommit)
	}
	file, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	fmt.Printf("SHA-256: %x\n", hash.Sum(nil))
	return nil
}

func stateDirectory() (string, error) {
	if d := os.Getenv("BABEL_TOWER_STATE"); d != "" {
		return d, nil
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "DeadlockAITranslator"), nil
		}
	}
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "DeadlockAITranslator"), nil
}
