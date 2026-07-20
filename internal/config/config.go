// Package config 从 config.json 加载配置
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config 项目配置，与 config.json 结构一致
type Config struct {
	Workspace    string `json:"workspace"`
	MemoryDir    string `json:"memory_dir"`
	DBPath       string `json:"db_path"`
	PicoclawBin  string `json:"picoclaw_bin"`
	LLMModel     string `json:"llm_model"`
	RetentionDays struct {
		Daily   int `json:"daily"`
		Weekly  int `json:"weekly"`
		Monthly int `json:"monthly"`
	} `json:"retention_days"`
}

// Load 从指定路径或默认路径加载配置
// 查找顺序：
// 1. 指定路径
// 2. 当前目录下的 config.json
// 3. 可执行文件同目录下的 config.json
func Load(configPath string) (*Config, error) {
	if configPath != "" {
		return loadFile(configPath)
	}

	// 当前目录
	if _, err := os.Stat("config.json"); err == nil {
		return loadFile("config.json")
	}

	// 可执行文件同目录
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		cfgPath := filepath.Join(exeDir, "config.json")
		if _, err := os.Stat(cfgPath); err == nil {
			return loadFile(cfgPath)
		}
	}

	return nil, fmt.Errorf("config.json not found")
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}
