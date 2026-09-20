package database

// 阶段 5 —— 全文检索索引（SQLite FTS5）。
// 阶段 6 —— 中文 bigram 分词 + 检索（见 entry_search.go 的 SearchEntries）。
//
// FTS5 虚拟表与同步触发器无法由 GORM 的 AutoMigrate 创建（见 PLAN.md「两个要留意的取舍」），
// 因此这里在 Install() 里用裸 SQL 单独落地，不指望 ORM 代劳。
//
// entries_fts 采用「外部内容表」（content='entries'）：索引本身不存正文副本，
// rowid 一一对应 entries.id，查询时回表取最新值，避免正表与索引两份数据漂移。
// 被索引列为 title / summary / content / author；tags 是 JSON 数组字符串，暂不纳入。
//
// 中文分词采用 bigram：入库前把连续中文串切成相邻双字，交给 FTS5 默认的 unicode61
// 分词器按空格再切词，从而让每个双字成为独立 token 可被命中。切词由注册进 SQLite 的
// bigram() 标量函数完成（见本文件底部），触发器/回填在写入索引时统一包一层 bigram()。

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/mattn/go-sqlite3"
)

const ftsTableName = "entries_fts"

// ftsSchemaVersion 标记 FTS 索引的 tokenization 版本。
// 1 = bigram 分词（阶段 6）；0 表示阶段 5 的原始（无中文分词）索引。
// 用 PRAGMA user_version 持久化：落后于当前版本即重建索引并回填。
const ftsSchemaVersion = 1

// bigramDriverName 是注册进 database/sql 的 sqlite 驱动名。
// 与默认 "sqlite3" 的区别：每次建立连接时通过 ConnectHook 注册 bigram SQL 函数，
// 供 FTS 触发器/回填在入库前做中文双字切词。连接池里每条新连接都会走一遍 ConnectHook。
const bigramDriverName = "sqlite3-bigram"

func init() {
	sql.Register(bigramDriverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("bigram", bigramSQL, true)
		},
	})
}

// ftsCreateSQL 建 FTS5 虚拟表（幂等）。分词器仍用默认 unicode61，
// 中文切词发生在写入值之前（bigram()），无需改 tokenizer。
const ftsCreateSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
	title, summary, content, author,
	content='entries', content_rowid='id'
)`

// ftsBackfillSQL 建表时把存量未删除条目一次性灌入索引（先 bigram 切词）。
const ftsBackfillSQL = `INSERT INTO entries_fts(rowid, title, summary, content, author)
SELECT id, bigram(title), bigram(summary), bigram(content), bigram(author)
FROM entries WHERE deleted = 0`

// ftsTriggerDDL 是三个同步触发器的重建定义。每次启动重建（先 DROP 后 CREATE），
// 保证触发器定义与当前代码一致。name 仅用于 DROP，create 是单条 CREATE TRIGGER 语句。
// 所有写入索引的值都先过 bigram()，与查询侧的 ftsQuery() 保持同一套切词。
var ftsTriggerDDL = []struct {
	name   string
	create string
}{
	{
		// 插入：新条目直接写入索引。
		name: "entries_fts_ai",
		create: `CREATE TRIGGER entries_fts_ai AFTER INSERT ON entries BEGIN
	INSERT INTO entries_fts(rowid, title, summary, content, author)
	VALUES (new.id, bigram(new.title), bigram(new.summary), bigram(new.content), bigram(new.author));
END`,
	},
	{
		// 物理删除：从索引移除。外部内容表需回传旧值以定位倒排项。
		name: "entries_fts_ad",
		create: `CREATE TRIGGER entries_fts_ad AFTER DELETE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, title, summary, content, author)
	VALUES ('delete', old.id, bigram(old.title), bigram(old.summary), bigram(old.content), bigram(old.author));
END`,
	},
	{
		// 更新：先删旧索引，再回插新值。
		// 本项目条目用 deleted 布尔软删（不物理 DELETE），因此 AFTER DELETE 实际不会触发；
		// 软删除走 UPDATE，这里在 new.deleted=1 时只删不插，保证软删条目不再可检索。
		name: "entries_fts_au",
		create: `CREATE TRIGGER entries_fts_au AFTER UPDATE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, title, summary, content, author)
	VALUES ('delete', old.id, bigram(old.title), bigram(old.summary), bigram(old.content), bigram(old.author));
	INSERT INTO entries_fts(rowid, title, summary, content, author)
	SELECT new.id, bigram(new.title), bigram(new.summary), bigram(new.content), bigram(new.author)
	WHERE new.deleted = 0;
END`,
	},
}

// installFTS 建 FTS5 虚拟表、同步触发器，并在表缺失或 tokenization 版本落后时
// 重建索引 + 回填存量条目。每次启动随 Install() 调用，必须幂等：
// 表已是最新版本时不再重建，触发器每次重建以保证与代码一致。
func (d *DB) installFTS() error {
	ver, err := d.userVersion()
	if err != nil {
		return err
	}
	exists, err := d.ftsTableExists()
	if err != nil {
		return err
	}

	if !exists || ver < ftsSchemaVersion {
		// 首次安装或从阶段 5 升级：先清旧触发器，再重建表 + 回填（bigram）。
		if err := d.dropFTSTriggers(); err != nil {
			return err
		}
		if err := d.Exec("DROP TABLE IF EXISTS " + ftsTableName).Error; err != nil {
			return err
		}
		if err := d.Exec(ftsCreateSQL).Error; err != nil {
			return err
		}
		if err := d.Exec(ftsBackfillSQL).Error; err != nil {
			return err
		}
		if err := d.setUserVersion(ftsSchemaVersion); err != nil {
			return err
		}
	}

	// 触发器每次启动重建（DROP 再 CREATE），保证定义与当前代码一致。
	if err := d.dropFTSTriggers(); err != nil {
		return err
	}
	for _, tr := range ftsTriggerDDL {
		if err := d.Exec(tr.create).Error; err != nil {
			return err
		}
	}
	return nil
}

// dropFTSTriggers 删除三个同步触发器（若存在）。
func (d *DB) dropFTSTriggers() error {
	for _, tr := range ftsTriggerDDL {
		if err := d.Exec("DROP TRIGGER IF EXISTS " + tr.name).Error; err != nil {
			return err
		}
	}
	return nil
}

// ftsTableExists 判断 FTS 虚拟表是否已存在。
func (d *DB) ftsTableExists() (bool, error) {
	var count int64
	err := d.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", ftsTableName).Scan(&count).Error
	return count > 0, err
}

// userVersion 读取 SQLite 的 PRAGMA user_version（FTS tokenization 版本）。
func (d *DB) userVersion() (int64, error) {
	var v int64
	err := d.Raw("PRAGMA user_version").Scan(&v).Error
	return v, err
}

// setUserVersion 写入 SQLite 的 PRAGMA user_version。PRAGMA 赋值不支持绑定参数，
// 用常量拼接（无外部输入，无注入风险）。
func (d *DB) setUserVersion(v int64) error {
	return d.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)).Error
}

// bigramSQL 是注册进 SQLite 的 bigram 标量函数（纯函数，结果仅依赖入参）。
// 入参用 any 承接：mattn/go-sqlite3 把 TEXT 转成 string、BLOB 转成 []byte、
// NULL 转成 nil []byte，这里统一按空串处理。
func bigramSQL(v any) string {
	switch t := v.(type) {
	case string:
		return bigramTokenize(t)
	case []byte:
		return bigramTokenize(string(t))
	default:
		return ""
	}
}

// bigramTokenize 把文本里的连续中文切成相邻双字（bigram），其余内容原样保留。
// 例如 "苹果iPhone发布" → "苹果 iPhone 发布"；结果再由 FTS5 的 unicode61 分词器
// 按空格切词。查询侧 ftsQuery() 用同一函数切词，保证索引与查询 token 一致。
func bigramTokenize(s string) string {
	runes := []rune(s)
	var b strings.Builder
	i := 0
	for i < len(runes) {
		if unicode.Is(unicode.Han, runes[i]) {
			j := i
			for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			run := runes[i:j]
			for k := 0; k+1 < len(run); k++ {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteRune(run[k])
				b.WriteRune(run[k+1])
			}
			if len(run) == 1 {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteRune(run[0])
			}
			i = j
		} else {
			j := i
			for j < len(runes) && !unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			b.WriteString(string(runes[i:j]))
			i = j
		}
	}
	return b.String()
}
