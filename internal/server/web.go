package server

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ma6254/news-glean/internal/webui"
)

// mountWeb 根据 web.mode 决定前端如何接入（见 config.WebConfig.Mode）。
//
//   - off：不提供前端，仅 /api 与 /swagger
//   - embed：内嵌二进制（webui 包 go:embed 的 dist）
//   - proxy：反向代理到外部前端服务（开发时指向 Vite dev server）
//   - dir：从本地目录提供静态文件
//   - gz：从 .tar.gz 打包文件解包后提供
func (s *Server) mountWeb(mux *http.ServeMux) {
	switch s.cfg.Web.Mode {
	case "off":
		return
	case "proxy":
		target, err := url.Parse(s.cfg.Web.ProxyURL)
		if err != nil {
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "web proxy url: "+err.Error(), http.StatusInternalServerError)
			}))
			return
		}
		mux.Handle("/", httputil.NewSingleHostReverseProxy(target))
	case "dir":
		mux.Handle("/", spaFileServer(http.Dir(s.cfg.Web.Dir)))
	case "gz":
		dir, err := extractTarGz(s.cfg.Web.Archive)
		if err != nil {
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "web archive: "+err.Error(), http.StatusInternalServerError)
			}))
			return
		}
		mux.Handle("/", spaFileServer(http.Dir(dir)))
	case "embed", "":
		dist, err := webui.Dist()
		if err != nil {
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "web embed: "+err.Error(), http.StatusInternalServerError)
			}))
			return
		}
		mux.Handle("/", spaFileServer(http.FS(dist)))
	default:
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, fmt.Sprintf("unknown web mode %q", s.cfg.Web.Mode), http.StatusInternalServerError)
		}))
	}
}

// extractTarGz 把 .tar.gz 解包到临时目录并返回其路径（用于 gz 模式）。
// 目录由操作系统临时目录管理，进程退出后由系统回收。
func extractTarGz(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	dir, err := os.MkdirTemp("", "newsglean-web-*")
	if err != nil {
		return "", err
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(dir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		}
	}
	return dir, nil
}

// spaFileServer 包装静态文件服务，支持前端 history 路由（BrowserRouter）：
// 请求命中真实文件时正常返回；未命中且路径不含扩展名时回退到 index.html（SPA 入口）。
// 含扩展名的缺失文件（如 /assets/x.js）仍返回 404，避免把 HTML 当静态资源返回。
func spaFileServer(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(r.URL.Path, "/")
		if upath == "" {
			upath = "index.html"
		}
		if f, err := fsys.Open(upath); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(upath, ".") {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
