package database

// 阶段 5 —— 全文检索索引（SQLite FTS5）。
//
// FTS5 虚拟表与同步触发器无法由 GORM 的 AutoMigrate 创建（见 PLAN.md「两个要留意的取舍」），
// 因此这里在 Install() 里用裸 SQL 单独落地，不指望 ORM 代劳。
//
// entries_fts 采用「外部内容表」（content='entries'）：索引本身不存正文副本，
// rowid 一一对应 entries.id，查询时回表取最新值，避免正表与索引两份数据漂移。
// 被索引列为 title / summary / content / author；tags 是 JSON 数组字符串，暂不纳入。
//
// 分词器暂用 FTS5 默认（unicode61），中文分词（bigram/trigram）属阶段 6，
// 届时按需重建本表即可，不影响此处的触发器与同步逻辑。

const ftsTableName = "entries_fts"

// ftsCreateSQL 建 FTS5 虚拟表（幂等）。
const ftsCreateSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
	title, summary, content, author,
	content='entries', content_rowid='id'
)`

// ftsBackfillSQL 首次建表时把存量未删除条目一次性灌入索引。
// 仅在 entries_fts 不存在时执行，保证幂等。
const ftsBackfillSQL = `INSERT INTO entries_fts(rowid, title, summary, content, author)
SELECT id, title, summary, content, author FROM entries WHERE deleted = 0`

// ftsTriggerDDL 是三个同步触发器的重建定义。每次启动重建（先 DROP 后 CREATE），
// 保证触发器定义与当前代码一致。name 仅用于 DROP，create 是单条 CREATE TRIGGER 语句。
var ftsTriggerDDL = []struct {
	name   string
	create string
}{
	{
		// 插入：新条目直接写入索引。
		name: "entries_fts_ai",
		create: `CREATE TRIGGER entries_fts_ai AFTER INSERT ON entries BEGIN
	INSERT INTO entries_fts(rowid, title, summary, content, author)
	VALUES (new.id, new.title, new.summary, new.content, new.author);
END`,
	},
	{
		// 物理删除：从索引移除。外部内容表需回传旧值以定位倒排项。
		name: "entries_fts_ad",
		create: `CREATE TRIGGER entries_fts_ad AFTER DELETE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, title, summary, content, author)
	VALUES ('delete', old.id, old.title, old.summary, old.content, old.author);
END`,
	},
	{
		// 更新：先删旧索引，再回插新值。
		// 本项目条目用 deleted 布尔软删（不物理 DELETE），因此 AFTER DELETE 实际不会触发；
		// 软删除走 UPDATE，这里在 new.deleted=1 时只删不插，保证软删条目不再可检索。
		name: "entries_fts_au",
		create: `CREATE TRIGGER entries_fts_au AFTER UPDATE ON entries BEGIN
	INSERT INTO entries_fts(entries_fts, rowid, title, summary, content, author)
	VALUES ('delete', old.id, old.title, old.summary, old.content, old.author);
	INSERT INTO entries_fts(rowid, title, summary, content, author)
	SELECT new.id, new.title, new.summary, new.content, new.author
	WHERE new.deleted = 0;
END`,
	},
}

// installFTS 建 FTS5 虚拟表、同步触发器，并在首次建表时回填存量条目。
// 每次启动随 Install() 调用，因此必须幂等：建表与回填仅在表不存在时执行，
// 触发器每次重建以保证与代码一致。
func (d *DB) installFTS() error {
	exists, err := d.ftsTableExists()
	if err != nil {
		return err
	}
	if !exists {
		if err := d.Exec(ftsCreateSQL).Error; err != nil {
			return err
		}
		if err := d.Exec(ftsBackfillSQL).Error; err != nil {
			return err
		}
	}
	for _, tr := range ftsTriggerDDL {
		if err := d.Exec("DROP TRIGGER IF EXISTS " + tr.name).Error; err != nil {
			return err
		}
		if err := d.Exec(tr.create).Error; err != nil {
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
