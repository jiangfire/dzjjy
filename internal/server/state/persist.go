package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"log/slog"
)

// StateStore 状态存储管理器
type StateStore struct {
	mu        sync.RWMutex
	stateFile string
	lockFile  *os.File
	log       *slog.Logger
}

// NewStateStore 创建状态存储
func NewStateStore(stateFile string) *StateStore {
	if stateFile == "" {
		stateFile = DefaultStateFile
	}
	return &StateStore{
		stateFile: stateFile,
		log:       slog.Default().With("module", "state"),
	}
}

// acquireLock 获取文件锁（原子性保证）
func (s *StateStore) acquireLock() error {
	lockPath := s.stateFile + ".lock"

	// 使用 O_EXCL 创建，如果已存在则失败（原子操作）
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0600) // #nosec G304 - lockPath derived from controlled stateFile
	if err != nil {
		return fmt.Errorf("failed to acquire lock, another process may be persisting: %w", err)
	}

	s.lockFile = lockFile
	return nil
}

// releaseLock 释放文件锁
func (s *StateStore) releaseLock() {
	if s.lockFile != nil {
		lockPath := s.lockFile.Name()
		if err := s.lockFile.Close(); err != nil {
			s.log.Warn("failed to close lock file", "error", err)
		}
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) { // #nosec G703 - lockPath is derived from configured stateFile
			s.log.Warn("failed to remove lock file", "error", err)
		}
		s.lockFile = nil
	}
}

// calculateChecksum 计算数据的SHA256校验和
func (s *StateStore) calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// verifyChecksum 验证校验和
func (s *StateStore) verifyChecksum(data []byte, expectedChecksum string) bool {
	actualChecksum := s.calculateChecksum(data)
	return actualChecksum == expectedChecksum
}

// AtomicWriteFile 原子写入文件
// 使用临时文件 + 重命名确保写入的原子性
func (s *StateStore) AtomicWriteFile(path string, data []byte) error {
	// 创建临时文件路径
	dir := filepath.Dir(path)
	tmpPath := filepath.Join(dir, ".tmp."+filepath.Base(path))

	// 1. 写入临时文件
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// 2. 确保临时文件已刷盘
	tmpFile, err := os.OpenFile(tmpPath, os.O_SYNC, 0600) // #nosec G304 - tmpPath derived from controlled path
	if err == nil {
		if syncErr := tmpFile.Sync(); syncErr != nil {
			slog.Warn("failed to sync temp file", "error", syncErr)
		}
		if closeErr := tmpFile.Close(); closeErr != nil {
			slog.Warn("failed to close temp file", "error", closeErr)
		}
	}

	// 3. 原子重命名（Unix系统保证原子性）
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(tmpPath); removeErr != nil { // 清理临时文件
			slog.Warn("failed to remove temp file", "error", removeErr)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// Persist 持久化状态数据
func (s *StateStore) Persist(data *StateData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取锁
	if err := s.acquireLock(); err != nil {
		return err
	}
	defer s.releaseLock()

	// 构建状态文件
	stateFile := StateFile{
		Version:   StateFileVersion,
		Timestamp: time.Now().Unix(),
		Data:      *data,
	}

	// 序列化
	jsonData, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// 计算校验和
	checksum := s.calculateChecksum(jsonData)
	stateFile.Checksum = checksum

	// 重新序列化（包含校验和）
	jsonData, err = json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state with checksum: %w", err)
	}

	// 原子写入
	if err := s.AtomicWriteFile(s.stateFile, jsonData); err != nil {
		return fmt.Errorf("failed to atomic write state: %w", err)
	}

	s.log.Info("state persisted", "file", s.stateFile, "apps", len(data.Apps))
	return nil
}

// Load 加载状态数据
func (s *StateStore) Load() (*StateFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 检查文件是否存在
	if _, err := os.Stat(s.stateFile); os.IsNotExist(err) {
		return nil, nil // 首次启动，无状态文件
	} else if err != nil {
		return nil, fmt.Errorf("failed to check state file: %w", err)
	}

	// 读取文件
	jsonData, err := os.ReadFile(s.stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// 解析状态文件
	var stateFile StateFile
	if err := json.Unmarshal(jsonData, &stateFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// 验证校验和：重新计算不包含 checksum 字段的数据的校验和
	storedChecksum := stateFile.Checksum
	stateFile.Checksum = "" // 清空 checksum 以便重新计算
	dataWithoutChecksum, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state for checksum: %w", err)
	}

	if !s.verifyChecksum(dataWithoutChecksum, storedChecksum) {
		// 尝试从备份恢复
		s.log.Warn("state file checksum mismatch, attempting backup recovery")
		return s.restoreFromBackup()
	}

	s.log.Info("state loaded", "file", s.stateFile, "apps", len(stateFile.Data.Apps))
	return &stateFile, nil
}

// Backup 创建备份
func (s *StateStore) Backup() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.stateFile); os.IsNotExist(err) {
		return nil // 无文件可备份
	}

	backupFile := s.stateFile + ".backup." + time.Now().Format("20060102-150405")
	data, err := os.ReadFile(s.stateFile)
	if err != nil {
		return err
	}

	return os.WriteFile(backupFile, data, 0600)
}

// restoreFromBackup 从备份恢复
func (s *StateStore) restoreFromBackup() (*StateFile, error) {
	// 查找最新的备份文件
	matches, err := filepath.Glob(s.stateFile + ".backup.*")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("no backup files found")
	}

	// 读取最新备份
	latestBackup := matches[len(matches)-1]
	data, err := os.ReadFile(latestBackup) // #nosec G304 - path from filepath.Glob of controlled stateFile
	if err != nil {
		return nil, fmt.Errorf("failed to read backup: %w", err)
	}

	var stateFile StateFile
	if err := json.Unmarshal(data, &stateFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup: %w", err)
	}

	// 验证备份的校验和
	storedChecksum := stateFile.Checksum
	stateFile.Checksum = ""
	dataWithoutChecksum, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backup for checksum: %w", err)
	}

	if !s.verifyChecksum(dataWithoutChecksum, storedChecksum) {
		return nil, fmt.Errorf("backup file is also corrupted")
	}

	s.log.Info("restored from backup", "file", latestBackup)

	// 用备份替换损坏的主文件
	if err := os.WriteFile(s.stateFile, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write state file: %w", err)
	}

	return &stateFile, nil
}

// Clear 清除状态文件
func (s *StateStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 删除主文件
	if err := os.Remove(s.stateFile); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 删除备份文件
	matches, _ := filepath.Glob(s.stateFile + ".backup.*")
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			s.log.Warn("failed to remove backup file", "file", match, "error", err)
		}
	}

	s.log.Info("state cleared")
	return nil
}

// Exists 检查状态文件是否存在
func (s *StateStore) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, err := os.Stat(s.stateFile)
	return err == nil
}
