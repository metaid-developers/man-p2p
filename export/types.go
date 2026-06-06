package export

import (
	"strings"

	"man-p2p/pin"
)

type ExportRequest struct {
	Identity     string `json:"identity"`
	IdentityType string `json:"identity_type"`
	StartHeight  int64  `json:"start_height"`
	EndHeight    int64  `json:"end_height"`
}

type PinRecord struct {
	ID             string `json:"id"`
	MetaId         string `json:"metaid,omitempty"`
	GlobalMetaId   string `json:"globalMetaId,omitempty"`
	Address        string `json:"address,omitempty"`
	Path           string `json:"path"`
	Operation      string `json:"operation,omitempty"`
	GenesisHeight  int64  `json:"genesisHeight"`
	Timestamp      int64  `json:"timestamp"`
	ChainName      string `json:"chainName"`
	ContentType    string `json:"contentType"`
	Content        string `json:"content,omitempty"`
	ContentSummary string `json:"contentSummary,omitempty"`
	Status         int    `json:"status"`
}

type ExportMeta struct {
	ExportVersion    int            `json:"exportVersion"`
	ExportedAt       int64          `json:"exportedAt"`
	Identity         string         `json:"identity"`
	IdentityType     string         `json:"identityType"`
	BlockRange       BlockRange     `json:"blockRange"`
	TotalPins        int            `json:"totalPins"`
	MonthlyBreakdown []MonthSummary `json:"monthlyBreakdown"`
}

type BlockRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type MonthSummary struct {
	Month    string   `json:"month"`
	PinCount int      `json:"pinCount"`
	Paths    []string `json:"paths"`
}

type TimelineEntry struct {
	Month      string `json:"month"`
	PinCount   int    `json:"pinCount"`
	FirstBlock int64  `json:"firstBlock"`
	LastBlock  int64  `json:"lastBlock"`
}

type MonthIndex struct {
	Month    string           `json:"month"`
	PinCount int              `json:"pinCount"`
	Paths    []PathIndexEntry `json:"paths"`
}

type PathIndexEntry struct {
	Path  string `json:"path"`
	File  string `json:"file"`
	Count int    `json:"count"`
}

func pathToFile(p string) string {
	s := strings.TrimPrefix(p, "/")
	s = strings.ReplaceAll(s, "/", "_")
	return s + ".json"
}

func isFileContent(path string) bool {
	return strings.HasPrefix(path, "/file")
}

func pinToRecord(p *pin.PinInscription) PinRecord {
	var content string
	var contentSummary string
	if isFileContent(p.Path) {
		contentSummary = p.ContentSummary
	} else {
		content = string(p.ContentBody)
	}
	return PinRecord{
		ID:             p.Id,
		MetaId:         p.MetaId,
		GlobalMetaId:   p.GlobalMetaId,
		Address:        p.Address,
		Path:           p.Path,
		Operation:      p.Operation,
		GenesisHeight:  p.GenesisHeight,
		Timestamp:      p.Timestamp,
		ChainName:      p.ChainName,
		ContentType:    p.ContentType,
		Content:        content,
		ContentSummary: contentSummary,
		Status:         p.Status,
	}
}
