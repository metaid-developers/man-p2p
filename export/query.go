package export

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/pebblestore"
	"man-p2p/pin"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

func QueryUserPins(db *pebblestore.Database, identity, identityType string, startHeight, endHeight int64) ([]*pin.PinInscription, error) {
	metaIds, err := resolveMetaIds(db, identity, identityType)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var result []*pin.PinInscription
	for _, metaId := range metaIds {
		pins, err := queryByMetaId(db, metaId, startHeight, endHeight, seen)
		if err != nil {
			return nil, err
		}
		result = append(result, pins...)
	}
	return result, nil
}

func resolveMetaIds(db *pebblestore.Database, identity, identityType string) ([]string, error) {
	switch identityType {
	case "address":
		return []string{common.GetMetaIdByAddress(identity)}, nil
	case "global_meta_id":
		return resolveByGlobalMetaId(db, identity)
	default:
		return nil, fmt.Errorf("unknown identity_type: %s", identityType)
	}
}

func resolveByGlobalMetaId(db *pebblestore.Database, globalMetaId string) ([]string, error) {
	it, err := db.MetaidInfoDB.NewIter(nil)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var result []string
	for it.First(); it.Valid(); it.Next() {
		var info pin.MetaIdInfo
		if err := sonic.Unmarshal(it.Value(), &info); err != nil {
			continue
		}
		if info.GlobalMetaId == globalMetaId {
			result = append(result, info.MetaId)
		}
	}
	return result, nil
}

func queryByMetaId(db *pebblestore.Database, metaId string, startHeight, endHeight int64, seen map[string]bool) ([]*pin.PinInscription, error) {
	prefix := metaId + "&"
	upperBound := metaId + "&\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"
	it, err := db.AddressDB.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(upperBound),
	})
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var result []*pin.PinInscription
	for it.First(); it.Valid(); it.Next() {
		key := string(it.Key())
		parts := strings.Split(key, "&")
		if len(parts) < 4 {
			continue
		}
		segments := strings.Split(parts[2], "_")
		if len(segments) < 3 {
			continue
		}
		height, err := strconv.ParseInt(segments[2], 10, 64)
		if err != nil {
			continue
		}
		if height < startHeight || height > endHeight {
			continue
		}
		pinId := parts[3]
		if seen[pinId] {
			continue
		}
		seen[pinId] = true

		raw, err := db.GetPinByKey(pinId)
		if err != nil {
			continue
		}
		var pinNode pin.PinInscription
		if err := sonic.Unmarshal(raw, &pinNode); err != nil {
			continue
		}
		result = append(result, &pinNode)
	}
	return result, nil
}
