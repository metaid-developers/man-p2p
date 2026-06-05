package export

import (
	"archive/zip"
	"encoding/json"
	"io"
	"sort"
	"time"

	"man-p2p/pin"
)

type monthGroup map[string]map[string][]PinRecord

func WriteArchive(w io.Writer, pins []*pin.PinInscription, req *ExportRequest) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	records := make([]PinRecord, len(pins))
	for i, p := range pins {
		records[i] = pinToRecord(p)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp < records[j].Timestamp
	})

	monthly := groupByMonth(records)

	if err := writeMeta(zw, req, records, monthly); err != nil {
		return err
	}

	if err := writeTimeline(zw, records); err != nil {
		return err
	}

	for _, month := range sortedMonths(monthly) {
		groups := monthly[month]
		if err := writeMonthIndex(zw, month, groups); err != nil {
			return err
		}
		for _, path := range sortedPaths(groups) {
			if err := writePathFile(zw, month, path, groups[path]); err != nil {
				return err
			}
		}
	}

	return nil
}

func groupByMonth(records []PinRecord) monthGroup {
	result := make(monthGroup)
	for _, r := range records {
		month := time.Unix(r.Timestamp, 0).Format("2006-01")
		if result[month] == nil {
			result[month] = make(map[string][]PinRecord)
		}
		result[month][r.Path] = append(result[month][r.Path], r)
	}
	return result
}

func sortedMonths(m monthGroup) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPaths(groups map[string][]PinRecord) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeMeta(zw *zip.Writer, req *ExportRequest, records []PinRecord, monthly monthGroup) error {
	f, err := zw.Create("export.json")
	if err != nil {
		return err
	}

	var minHeight, maxHeight int64
	if len(records) > 0 {
		minHeight = records[0].GenesisHeight
		maxHeight = records[0].GenesisHeight
		for _, r := range records {
			if r.GenesisHeight < minHeight {
				minHeight = r.GenesisHeight
			}
			if r.GenesisHeight > maxHeight {
				maxHeight = r.GenesisHeight
			}
		}
	}

	var breakdown []MonthSummary
	for _, month := range sortedMonths(monthly) {
		groups := monthly[month]
		var total int
		var paths []string
		for _, p := range sortedPaths(groups) {
			total += len(groups[p])
			paths = append(paths, p)
		}
		breakdown = append(breakdown, MonthSummary{
			Month:    month,
			PinCount: total,
			Paths:    paths,
		})
	}

	meta := ExportMeta{
		ExportVersion: 1,
		ExportedAt:    time.Now().Unix(),
		Identity:      req.Identity,
		IdentityType:  req.IdentityType,
		BlockRange: BlockRange{
			Start: minHeight,
			End:   maxHeight,
		},
		TotalPins:        len(records),
		MonthlyBreakdown: breakdown,
	}

	return json.NewEncoder(f).Encode(meta)
}

func writeTimeline(zw *zip.Writer, records []PinRecord) error {
	f, err := zw.Create("timeline.json")
	if err != nil {
		return err
	}

	byMonth := make(map[string][]PinRecord)
	for _, r := range records {
		month := time.Unix(r.Timestamp, 0).Format("2006-01")
		byMonth[month] = append(byMonth[month], r)
	}

	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)

	var entries []TimelineEntry
	for _, m := range months {
		recs := byMonth[m]
		var firstBlock, lastBlock int64
		if len(recs) > 0 {
			firstBlock = recs[0].GenesisHeight
			lastBlock = recs[0].GenesisHeight
			for _, r := range recs {
				if r.GenesisHeight < firstBlock {
					firstBlock = r.GenesisHeight
				}
				if r.GenesisHeight > lastBlock {
					lastBlock = r.GenesisHeight
				}
			}
		}
		entries = append(entries, TimelineEntry{
			Month:      m,
			PinCount:   len(recs),
			FirstBlock: firstBlock,
			LastBlock:  lastBlock,
		})
	}

	return json.NewEncoder(f).Encode(map[string]interface{}{
		"totalPins":       len(records),
		"monthlyActivity": entries,
	})
}

func writeMonthIndex(zw *zip.Writer, month string, groups map[string][]PinRecord) error {
	f, err := zw.Create(month + "/_index.json")
	if err != nil {
		return err
	}

	var total int
	var entries []PathIndexEntry
	for _, path := range sortedPaths(groups) {
		recs := groups[path]
		total += len(recs)
		entries = append(entries, PathIndexEntry{
			Path:  path,
			File:  pathToFile(path),
			Count: len(recs),
		})
	}

	idx := MonthIndex{
		Month:    month,
		PinCount: total,
		Paths:    entries,
	}

	return json.NewEncoder(f).Encode(idx)
}

func writePathFile(zw *zip.Writer, month, path string, records []PinRecord) error {
	f, err := zw.Create(month + "/" + pathToFile(path))
	if err != nil {
		return err
	}

	return json.NewEncoder(f).Encode(records)
}
