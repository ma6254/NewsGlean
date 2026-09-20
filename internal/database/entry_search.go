package database

// 阶段 6 —— 全文检索（SearchEntries）。
//
// 检索做成驱动无关的数据层契约：上层（HTTP / 核心层）只调 SearchEntries，
// 不感知底层是 sqlite FTS5 还是 mysql FULLTEXT。中文分词统一按 bigram 切词，
// 与 MySQL 内置 ngram parser（默认 ngram_token_size=2）行为对齐。

import (
	"errors"
	"strings"
)

// ErrEmptyQuery 表示搜索关键词去空白/分词后无有效 token。
var ErrEmptyQuery = errors.New("empty search query")

// SearchEntries 全文检索未删除条目，命中 title/summary/content/author。
// 中文按 bigram 切词，多词按 AND 匹配；结果按发布时间倒序、ID 倒序分页。
// 过滤条件复用 EntryFilter（渠道 / 已读 / 收藏 / 归档）。
func (d *DB) SearchEntries(q string, f EntryFilter, page, pageSize int) ([]Entry, int64, error) {
	if d.driver != "sqlite" {
		return nil, 0, errors.New("search not implemented for driver " + d.driver)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	match := ftsQuery(q)
	if match == "" {
		return nil, 0, ErrEmptyQuery
	}

	base := d.Model(&Entry{}).
		Joins("JOIN entries_fts ON entries_fts.rowid = entries.id").
		Where("entries.deleted = ?", false).
		Where("entries_fts MATCH ?", match)
	base = applyEntryFilter(base, f)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Entry
	err := base.Order("entries.published_at DESC, entries.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// ftsQuery 把用户输入转成 FTS5 MATCH 表达式：bigram 切词后逐 token 加引号、
// 以空格连接（隐式 AND）。加引号是为了让 FTS5 的特殊字符按字面量处理。
func ftsQuery(q string) string {
	toks := strings.Fields(bigramTokenize(q))
	for i, t := range toks {
		toks[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(toks, " ")
}
