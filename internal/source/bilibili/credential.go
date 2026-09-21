package bilibili

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// credentialFile 是 bilibili-cli 的凭证文件结构（对齐 auth.py save_credential）。
type credentialFile struct {
	Sessdata    string `json:"sessdata"`
	BiliJct     string `json:"bili_jct"`
	AcTimeValue string `json:"ac_time_value"`
	Buvid3      string `json:"buvid3"`
	Buvid4      string `json:"buvid4"`
	Dedeuserid  string `json:"dedeuserid"`
	SavedAt     int64  `json:"saved_at"`
}

// CredentialPath 返回 bilibili-cli 的凭证文件路径（~/.bilibili-cli/credential.json）。
func CredentialPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bilibili-cli", "credential.json"), nil
}

// WriteCredential 把 SESSDATA/bili_jct（可选 buvid3）合并写入凭证文件，
// 保留已存在的其它字段（buvid4/dedeuserid/ac_time_value），0600 权限。返回写入路径。
func WriteCredential(sessdata, biliJct, buvid3 string) (string, error) {
	path, err := CredentialPath()
	if err != nil {
		return "", err
	}
	if err := writeCredentialFile(path, sessdata, biliJct, buvid3); err != nil {
		return "", err
	}
	return path, nil
}

// writeCredentialFile 合并写入凭证文件（可注入路径，便于测试）。
func writeCredentialFile(path, sessdata, biliJct, buvid3 string) error {
	var existing credentialFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	existing.Sessdata = sessdata
	existing.BiliJct = biliJct
	if buvid3 != "" {
		existing.Buvid3 = buvid3
	}
	existing.SavedAt = time.Now().Unix()

	data, err := json.MarshalIndent(&existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
