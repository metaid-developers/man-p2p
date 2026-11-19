package export

import (
	"fmt"
	"io"

	"man-p2p/man"
)

// maxPins 单次导出上限：导出走全库扫描 + 逐 pin 序列化，500000 会放大成
// 分钟级重活；SOP 公网验收样例（address 全量）实测仅 133 个文件，5000 足够。
const maxPins = 5000

func ExportUserData(w io.Writer, req *ExportRequest) error {
	if err := validateRequest(req); err != nil {
		return err
	}
	if man.PebbleStore.Database == nil {
		return fmt.Errorf("database not initialized")
	}
	pins, err := QueryUserPins(man.PebbleStore.Database, req.Identity, req.IdentityType, req.StartHeight, req.EndHeight)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	if len(pins) > maxPins {
		return fmt.Errorf("export exceeds maximum of %d pins (got %d)", maxPins, len(pins))
	}
	return WriteArchive(w, pins, req)
}

func validateRequest(req *ExportRequest) error {
	if req.Identity == "" {
		return fmt.Errorf("identity is required")
	}
	if req.IdentityType != "global_meta_id" && req.IdentityType != "address" {
		return fmt.Errorf("identity_type must be 'global_meta_id' or 'address'")
	}
	if req.StartHeight < 0 || req.EndHeight <= 0 {
		return fmt.Errorf("start_height must be non-negative and end_height must be positive")
	}
	if req.StartHeight > req.EndHeight {
		return fmt.Errorf("start_height must not exceed end_height")
	}
	return nil
}
