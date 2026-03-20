package man

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
)

// SnapshotMetadata 快照元数据
type SnapshotMetadata struct {
	ID           string           `json:"id"`            // 快照ID (时间戳格式)
	CreatedAt    time.Time        `json:"created_at"`    // 创建时间
	Description  string           `json:"description"`   // 快照描述
	ChainHeights map[string]int64 `json:"chain_heights"` // 各链的区块高度
	RecordCounts map[string]int64 `json:"record_counts"` // 各类记录数量
	FileSize     int64            `json:"file_size"`     // 快照文件大小
	Checksum     string           `json:"checksum"`      // 校验和
}

// SnapshotRecord 快照中的单条记录
type SnapshotRecord struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

// MRC20 数据前缀列表（需要快照的数据）
var mrc20DataPrefixes = []string{
	"mrc20_utxo_",          // UTXO 主记录
	"mrc20_tick_",          // 代币信息
	"mrc20_in_",            // 地址收入索引
	"available_utxo_",      // 可用 UTXO 索引
	"block_created_",       // 区块创建索引
	"pending_teleport_",    // Teleport 待处理记录
	"arrival_",             // Arrival 记录
	"teleport_pending_in_", // Teleport 待入账
	"transfer_pending_in_", // Transfer 待入账
	"account_balance_",     // 账户余额
	"mrc20_sync_height_",   // 同步高度
}

// CreateSnapshot 创建 MRC20 数据快照
// snapshotDir: 快照存储目录
// description: 快照描述
// 返回快照元数据
func (pd *PebbleData) CreateSnapshot(snapshotDir, description string) (*SnapshotMetadata, error) {
	startTime := time.Now()

	// 创建快照目录
	snapshotID := startTime.Format("2006-01-02_15-04-05")
	snapshotPath := filepath.Join(snapshotDir, snapshotID)
	if err := os.MkdirAll(snapshotPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	log.Printf("[Snapshot] Starting snapshot creation: %s", snapshotID)

	// 获取各链的同步高度
	chainHeights := make(map[string]int64)
	for _, chain := range []string{"btc", "doge", "mvc"} {
		height := pd.GetMrc20SyncHeight(chain)
		chainHeights[chain] = height
	}

	// 创建数据文件
	dataFilePath := filepath.Join(snapshotPath, "mrc20_data.gz")
	dataFile, err := os.Create(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create data file: %w", err)
	}
	defer dataFile.Close()

	// 使用 gzip 压缩
	gzWriter := gzip.NewWriter(dataFile)
	defer gzWriter.Close()

	encoder := json.NewEncoder(gzWriter)

	// 统计各类记录数量
	recordCounts := make(map[string]int64)
	totalRecords := int64(0)

	// 遍历所有前缀，导出数据
	for _, prefix := range mrc20DataPrefixes {
		count, err := pd.exportPrefixData(encoder, prefix)
		if err != nil {
			log.Printf("[Snapshot] Warning: failed to export %s: %v", prefix, err)
			continue
		}
		recordCounts[prefix] = count
		totalRecords += count
		log.Printf("[Snapshot] Exported %s: %d records", prefix, count)
	}

	// 关闭 gzip writer 以确保数据写入完成
	gzWriter.Close()
	dataFile.Close()

	// 获取文件大小
	fileInfo, err := os.Stat(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat data file: %w", err)
	}

	// 创建元数据
	metadata := &SnapshotMetadata{
		ID:           snapshotID,
		CreatedAt:    startTime,
		Description:  description,
		ChainHeights: chainHeights,
		RecordCounts: recordCounts,
		FileSize:     fileInfo.Size(),
	}

	// 保存元数据
	metadataPath := filepath.Join(snapshotPath, "metadata.json")
	metadataFile, err := os.Create(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer metadataFile.Close()

	metaEncoder := json.NewEncoder(metadataFile)
	metaEncoder.SetIndent("", "  ")
	if err := metaEncoder.Encode(metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("[Snapshot] Snapshot created successfully: %s", snapshotID)
	log.Printf("[Snapshot] Total records: %d, File size: %s, Duration: %v",
		totalRecords, formatBytes(fileInfo.Size()), duration)

	return metadata, nil
}

// exportPrefixData 导出指定前缀的数据
func (pd *PebbleData) exportPrefixData(encoder *json.Encoder, prefix string) (int64, error) {
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var count int64
	for iter.First(); iter.Valid(); iter.Next() {
		record := SnapshotRecord{
			Key:   string(iter.Key()),
			Value: string(iter.Value()),
		}
		if err := encoder.Encode(record); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

// RestoreSnapshot 从快照恢复数据
// snapshotPath: 快照目录路径
// 注意：此操作会清空现有 MRC20 数据！
func (pd *PebbleData) RestoreSnapshot(snapshotPath string) (*SnapshotMetadata, error) {
	startTime := time.Now()

	// 读取元数据
	metadataPath := filepath.Join(snapshotPath, "metadata.json")
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata file: %w", err)
	}
	defer metadataFile.Close()

	var metadata SnapshotMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	log.Printf("[Snapshot] Restoring from snapshot: %s (created: %s)",
		metadata.ID, metadata.CreatedAt.Format(time.RFC3339))
	log.Printf("[Snapshot] Chain heights: %v", metadata.ChainHeights)

	// Step 1: 清空现有 MRC20 数据
	log.Printf("[Snapshot] Step 1: Clearing existing MRC20 data...")
	if err := pd.clearAllMrc20Data(); err != nil {
		return nil, fmt.Errorf("failed to clear existing data: %w", err)
	}

	// Step 2: 导入快照数据
	log.Printf("[Snapshot] Step 2: Importing snapshot data...")
	dataFilePath := filepath.Join(snapshotPath, "mrc20_data.gz")
	importedCount, err := pd.importSnapshotData(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to import snapshot data: %w", err)
	}

	// Step 3: 恢复同步高度
	log.Printf("[Snapshot] Step 3: Restoring sync heights...")
	for chain, height := range metadata.ChainHeights {
		if err := pd.SetMrc20SyncHeight(chain, height); err != nil {
			log.Printf("[Snapshot] Warning: failed to set sync height for %s: %v", chain, err)
		}
	}

	duration := time.Since(startTime)
	log.Printf("[Snapshot] Restore completed successfully!")
	log.Printf("[Snapshot] Imported %d records in %v", importedCount, duration)

	return &metadata, nil
}

// clearAllMrc20Data 清空所有 MRC20 数据
func (pd *PebbleData) clearAllMrc20Data() error {
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	totalDeleted := int64(0)

	for _, prefix := range mrc20DataPrefixes {
		iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
			LowerBound: []byte(prefix),
			UpperBound: []byte(prefix + "~"),
		})
		if err != nil {
			continue
		}

		count := int64(0)
		for iter.First(); iter.Valid(); iter.Next() {
			if err := batch.Delete(iter.Key(), pebble.Sync); err != nil {
				iter.Close()
				return err
			}
			count++

			// 每 10000 条提交一次，避免 batch 过大
			if count%10000 == 0 {
				if err := batch.Commit(pebble.Sync); err != nil {
					iter.Close()
					return err
				}
				batch.Close()
				batch = pd.Database.MrcDb.NewBatch()
			}
		}
		iter.Close()

		totalDeleted += count
		log.Printf("[Snapshot] Cleared %s: %d records", prefix, count)
	}

	// 提交剩余的删除
	if err := batch.Commit(pebble.Sync); err != nil {
		return err
	}

	log.Printf("[Snapshot] Total cleared: %d records", totalDeleted)
	return nil
}

// importSnapshotData 导入快照数据
func (pd *PebbleData) importSnapshotData(dataFilePath string) (int64, error) {
	dataFile, err := os.Open(dataFilePath)
	if err != nil {
		return 0, err
	}
	defer dataFile.Close()

	gzReader, err := gzip.NewReader(dataFile)
	if err != nil {
		return 0, err
	}
	defer gzReader.Close()

	decoder := json.NewDecoder(gzReader)
	batch := pd.Database.MrcDb.NewBatch()
	defer batch.Close()

	var count int64
	for {
		var record SnapshotRecord
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			return count, err
		}

		if err := batch.Set([]byte(record.Key), []byte(record.Value), pebble.Sync); err != nil {
			return count, err
		}
		count++

		// 每 10000 条提交一次
		if count%10000 == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return count, err
			}
			batch.Close()
			batch = pd.Database.MrcDb.NewBatch()
			log.Printf("[Snapshot] Imported %d records...", count)
		}
	}

	// 提交剩余的数据
	if err := batch.Commit(pebble.Sync); err != nil {
		return count, err
	}

	return count, nil
}

// ListSnapshots 列出所有快照
func ListSnapshots(snapshotDir string) ([]SnapshotMetadata, error) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SnapshotMetadata{}, nil
		}
		return nil, err
	}

	var snapshots []SnapshotMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadataPath := filepath.Join(snapshotDir, entry.Name(), "metadata.json")
		metadataFile, err := os.Open(metadataPath)
		if err != nil {
			continue // 跳过无效的快照目录
		}

		var metadata SnapshotMetadata
		if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
			metadataFile.Close()
			continue
		}
		metadataFile.Close()

		snapshots = append(snapshots, metadata)
	}

	// 按创建时间倒序排列
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})

	return snapshots, nil
}

// DeleteSnapshot 删除快照
func DeleteSnapshot(snapshotDir, snapshotID string) error {
	snapshotPath := filepath.Join(snapshotDir, snapshotID)

	// 检查是否存在
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		return fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	return os.RemoveAll(snapshotPath)
}

// GetSnapshotInfo 获取快照信息
func GetSnapshotInfo(snapshotDir, snapshotID string) (*SnapshotMetadata, error) {
	metadataPath := filepath.Join(snapshotDir, snapshotID, "metadata.json")
	metadataFile, err := os.Open(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}
	defer metadataFile.Close()

	var metadata SnapshotMetadata
	if err := json.NewDecoder(metadataFile).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// GetMrc20SyncHeight 获取 MRC20 同步高度
func (pd *PebbleData) GetMrc20SyncHeight(chain string) int64 {
	key := fmt.Sprintf("mrc20_sync_height_%s", chain)
	value, closer, err := pd.Database.MrcDb.Get([]byte(key))
	if err != nil {
		return 0
	}
	defer closer.Close()

	var height int64
	fmt.Sscanf(string(value), "%d", &height)
	return height
}

// SetMrc20SyncHeight 设置 MRC20 同步高度
func (pd *PebbleData) SetMrc20SyncHeight(chain string, height int64) error {
	key := fmt.Sprintf("mrc20_sync_height_%s", chain)
	value := fmt.Sprintf("%d", height)
	return pd.Database.MrcDb.Set([]byte(key), []byte(value), pebble.Sync)
}

// formatBytes 格式化字节大小
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GetSnapshotDir 获取快照目录路径
func GetSnapshotDir(basePath string) string {
	return filepath.Join(basePath, "snapshots")
}

// VerifySnapshot 验证快照完整性
func VerifySnapshot(snapshotDir, snapshotID string) error {
	snapshotPath := filepath.Join(snapshotDir, snapshotID)

	// 检查元数据
	metadataPath := filepath.Join(snapshotPath, "metadata.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return fmt.Errorf("metadata.json not found")
	}

	// 检查数据文件
	dataFilePath := filepath.Join(snapshotPath, "mrc20_data.gz")
	if _, err := os.Stat(dataFilePath); os.IsNotExist(err) {
		return fmt.Errorf("mrc20_data.gz not found")
	}

	// 尝试读取数据文件验证完整性
	dataFile, err := os.Open(dataFilePath)
	if err != nil {
		return fmt.Errorf("cannot open data file: %w", err)
	}
	defer dataFile.Close()

	gzReader, err := gzip.NewReader(dataFile)
	if err != nil {
		return fmt.Errorf("invalid gzip format: %w", err)
	}
	defer gzReader.Close()

	// 快速验证：读取前 100 条记录
	decoder := json.NewDecoder(gzReader)
	for i := 0; i < 100; i++ {
		var record SnapshotRecord
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("invalid record at position %d: %w", i, err)
		}
		if record.Key == "" {
			return fmt.Errorf("empty key at position %d", i)
		}
	}

	return nil
}

// CleanupOldSnapshots 清理旧快照，保留最近 N 个
func CleanupOldSnapshots(snapshotDir string, keepCount int) ([]string, error) {
	snapshots, err := ListSnapshots(snapshotDir)
	if err != nil {
		return nil, err
	}

	if len(snapshots) <= keepCount {
		return nil, nil
	}

	var deleted []string
	for i := keepCount; i < len(snapshots); i++ {
		snapshotID := snapshots[i].ID
		if err := DeleteSnapshot(snapshotDir, snapshotID); err != nil {
			log.Printf("[Snapshot] Failed to delete old snapshot %s: %v", snapshotID, err)
			continue
		}
		deleted = append(deleted, snapshotID)
		log.Printf("[Snapshot] Deleted old snapshot: %s", snapshotID)
	}

	return deleted, nil
}

// ExportChainData 仅导出指定链的数据（用于单链回滚）
func (pd *PebbleData) ExportChainData(snapshotDir, chain, description string) (*SnapshotMetadata, error) {
	startTime := time.Now()

	snapshotID := fmt.Sprintf("%s_%s", chain, startTime.Format("2006-01-02_15-04-05"))
	snapshotPath := filepath.Join(snapshotDir, snapshotID)
	if err := os.MkdirAll(snapshotPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	log.Printf("[Snapshot] Starting chain-specific export: %s for chain %s", snapshotID, chain)

	// 获取该链的同步高度
	chainHeights := map[string]int64{
		chain: pd.GetMrc20SyncHeight(chain),
	}

	// 创建数据文件
	dataFilePath := filepath.Join(snapshotPath, "mrc20_data.gz")
	dataFile, err := os.Create(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create data file: %w", err)
	}
	defer dataFile.Close()

	gzWriter := gzip.NewWriter(dataFile)
	defer gzWriter.Close()

	encoder := json.NewEncoder(gzWriter)
	recordCounts := make(map[string]int64)
	totalRecords := int64(0)

	// 导出与该链相关的数据
	chainPrefixes := []string{
		fmt.Sprintf("available_utxo_%s_", chain),
		fmt.Sprintf("block_created_%s_", chain),
		fmt.Sprintf("mrc20_sync_height_%s", chain),
	}

	// 还需要导出通用数据中属于该链的记录
	for _, prefix := range chainPrefixes {
		count, err := pd.exportPrefixData(encoder, prefix)
		if err != nil {
			log.Printf("[Snapshot] Warning: failed to export %s: %v", prefix, err)
			continue
		}
		recordCounts[prefix] = count
		totalRecords += count
	}

	// 导出该链的 UTXO（需要过滤）
	utxoCount, err := pd.exportChainUtxos(encoder, chain)
	if err != nil {
		log.Printf("[Snapshot] Warning: failed to export UTXOs for %s: %v", chain, err)
	} else {
		recordCounts["mrc20_utxo_"+chain] = utxoCount
		totalRecords += utxoCount
	}

	gzWriter.Close()
	dataFile.Close()

	fileInfo, err := os.Stat(dataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat data file: %w", err)
	}

	metadata := &SnapshotMetadata{
		ID:           snapshotID,
		CreatedAt:    startTime,
		Description:  description,
		ChainHeights: chainHeights,
		RecordCounts: recordCounts,
		FileSize:     fileInfo.Size(),
	}

	metadataPath := filepath.Join(snapshotPath, "metadata.json")
	metadataFile, err := os.Create(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer metadataFile.Close()

	metaEncoder := json.NewEncoder(metadataFile)
	metaEncoder.SetIndent("", "  ")
	if err := metaEncoder.Encode(metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Printf("[Snapshot] Chain export completed: %s, records=%d, size=%s",
		snapshotID, totalRecords, formatBytes(fileInfo.Size()))

	return metadata, nil
}

// exportChainUtxos 导出指定链的 UTXO
func (pd *PebbleData) exportChainUtxos(encoder *json.Encoder, chain string) (int64, error) {
	prefix := "mrc20_utxo_"
	iter, err := pd.Database.MrcDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "~"),
	})
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var count int64
	for iter.First(); iter.Valid(); iter.Next() {
		// 检查是否属于目标链（需要解析 value）
		value := iter.Value()
		if !strings.Contains(string(value), `"chain":"`+chain+`"`) {
			continue
		}

		record := SnapshotRecord{
			Key:   string(iter.Key()),
			Value: string(value),
		}
		if err := encoder.Encode(record); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}
