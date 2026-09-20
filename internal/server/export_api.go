package server

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ma6254/news-glean/internal/export"
)

// handleExportMarkdown 处理 POST /api/export/markdown（按源/月份分目录导出为 Markdown）。
//
// @Summary      导出 Markdown
// @Description  将条目按「源/YYYY-MM」分目录导出为 Markdown（含 YAML front matter），重复导出幂等覆盖
// @Tags         export
// @Produce      json
// @Param        source_id  query     int   false  "按渠道过滤"
// @Param        read       query     bool  false  "按已读状态过滤"
// @Param        favorite   query     bool  false  "按收藏状态过滤"
// @Param        archive    query     bool  false  "按归档状态过滤"
// @Success      200        {object}  export.Result
// @Failure      400        {object}  ErrorResponse
// @Router       /export/markdown [post]
func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request) {
	filter, err := parseEntryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := export.ExportMarkdown(s.db, s.cfg.Export.MarkdownDir, export.Options{Filter: filter})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleExportJSON 处理 POST /api/export/json（导出单文件 JSON 数组供脚本消费）。
//
// @Summary      导出 JSON
// @Description  将条目导出为单个 JSON 数组文件（entries.json），tags/extra 展开为原生结构，重复导出幂等覆盖
// @Tags         export
// @Produce      json
// @Param        source_id  query     int   false  "按渠道过滤"
// @Param        read       query     bool  false  "按已读状态过滤"
// @Param        favorite   query     bool  false  "按收藏状态过滤"
// @Param        archive    query     bool  false  "按归档状态过滤"
// @Success      200        {object}  export.Result
// @Failure      400        {object}  ErrorResponse
// @Router       /export/json [post]
func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	filter, err := parseEntryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := export.ExportJSON(s.db, s.cfg.Export.JSONDir, export.Options{Filter: filter})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleExportEPUB 处理 POST /api/export/epub（每源一本 EPUB，按月份分章）。
//
// @Summary      导出 EPUB
// @Description  将条目按源导出为 EPUB（每源一本、按月份分章），容器用标准库 archive/zip 自写，重复导出幂等覆盖
// @Tags         export
// @Produce      json
// @Param        source_id  query     int   false  "按渠道过滤"
// @Param        read       query     bool  false  "按已读状态过滤"
// @Param        favorite   query     bool  false  "按收藏状态过滤"
// @Param        archive    query     bool  false  "按归档状态过滤"
// @Success      200        {object}  export.Result
// @Failure      400        {object}  ErrorResponse
// @Router       /export/epub [post]
func (s *Server) handleExportEPUB(w http.ResponseWriter, r *http.Request) {
	filter, err := parseEntryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := export.ExportEPUB(s.db, s.cfg.Export.EPUBDir, export.Options{Filter: filter})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleExportDownload 处理 GET /api/export/download（触发导出并作为文件流返回）。
//
// @Summary      导出并下载
// @Description  触发导出并返回文件流：markdown→zip、json→单文件、epub→单文件或多源打包 zip；过滤参数同列表
// @Tags         export
// @Produce      json
// @Param        format     query     string  true   "格式：markdown|json|epub"
// @Param        source_id  query     int     false  "按渠道过滤"
// @Param        read       query     bool    false  "按已读状态过滤"
// @Param        favorite   query     bool    false  "按收藏状态过滤"
// @Param        archive    query     bool    false  "按归档状态过滤"
// @Success      200        {file}    file
// @Failure      400        {object}  ErrorResponse
// @Router       /export/download [get]
func (s *Server) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	filter, err := parseEntryFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch format {
	case "markdown":
		result, err := export.ExportMarkdown(s.db, s.cfg.Export.MarkdownDir, export.Options{Filter: filter})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		serveZipDir(w, result.Dir, "news-glean-markdown.zip")
	case "json":
		result, err := export.ExportJSON(s.db, s.cfg.Export.JSONDir, export.Options{Filter: filter})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		serveFile(w, filepath.Join(result.Dir, "entries.json"), "news-glean-entries.json", "application/json")
	case "epub":
		result, err := export.ExportEPUB(s.db, s.cfg.Export.EPUBDir, export.Options{Filter: filter})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		epubs, err := listByExt(result.Dir, ".epub")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(epubs) == 1 {
			serveFile(w, filepath.Join(result.Dir, epubs[0]), epubs[0], "application/epub+zip")
			return
		}
		serveZipDir(w, result.Dir, "news-glean-epub.zip")
	default:
		writeError(w, http.StatusBadRequest, "invalid format")
	}
}

// serveFile 以附件形式返回单个文件。
func serveFile(w http.ResponseWriter, path, filename, contentType string) {
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = io.Copy(w, f)
}

// serveZipDir 把目录打包成 zip 并以附件形式返回。
// 响应头在打包前写出，故打包中的错误无法再改状态码，仅记录日志。
func serveZipDir(w http.ResponseWriter, dir, filename string) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := export.WriteZipDir(w, dir); err != nil {
		log.Printf("export download zip %s: %v", dir, err)
	}
}

// listByExt 返回 dir 下指定扩展名的常规文件名（升序，确定性）。
func listByExt(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
