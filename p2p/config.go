package p2p

import (
	"context"
	"encoding/json"
	"os"
	"sync"
)

type P2PSyncConfig struct {
	SyncMode           string   `json:"p2p_sync_mode"`
	SelectiveAddresses []string `json:"p2p_selective_addresses"`
	SelectivePaths     []string `json:"p2p_selective_paths"`
	BlockAddresses     []string `json:"p2p_block_addresses"`
	BlockPaths         []string `json:"p2p_block_paths"`
	MaxContentSizeKB   int64    `json:"p2p_max_content_size_kb"`
	BootstrapNodes     []string `json:"p2p_bootstrap_nodes"`
	EnableRelay        bool     `json:"p2p_enable_relay"`
	ListenPort         int      `json:"p2p_listen_port"`
	AnnounceAddrs      []string `json:"p2p_announce_addrs"`
	StorageLimitGB     float64  `json:"p2p_storage_limit_gb"`
	OwnAddresses       []string `json:"p2p_own_addresses"`
	EnableChainSource  *bool    `json:"p2p_enable_chain_source"`
}

var (
	currentConfig P2PSyncConfig
	configPath    string
	configMu      sync.RWMutex
)

func LoadConfig(path string) error {
	configPath = path
	return ReloadConfig()
}

func ReloadConfig() error {
	if configPath == "" {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg P2PSyncConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	configMu.Lock()
	currentConfig = cfg
	configMu.Unlock()
	if Node != nil {
		go connectBootstrapNodes(context.Background())
	}
	return nil
}

func GetConfig() P2PSyncConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	return currentConfig
}

func (cfg P2PSyncConfig) ChainSourceEnabled() bool {
	if cfg.EnableChainSource == nil {
		return true
	}
	return *cfg.EnableChainSource
}
