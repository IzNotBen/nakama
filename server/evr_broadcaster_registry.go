package server

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/blugelabs/bluge"
	"github.com/blugelabs/bluge/search"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

// Represents identity information for a single match participant.
type BroadcasterPresence struct {
	ServerID    evr.Symbol   `json:"id,omitempty"`           // The server id of the broadcaster. (EVR)
	Endpoint    evr.Endpoint `json:"endpoint,omitempty"`     // The endpoint data used for connections.
	VersionLock evr.Symbol   `json:"version_lock,omitempty"` // The game build version. (EVR)
	AppID       int64        `json:"app_id,omitempty"`       // The app id of the broadcaster. (EVR)
	Region      evr.Symbol   `json:"region,omitempty"`       // The region the match is hosted in. (Matching Only) (EVR)
	Channels    []uuid.UUID  `json:"channels,omitempty"`     // The channels this broadcaster will host matches for.
	Tags        []string     `json:"tags,omitempty"`         // The tags given on the urlparam for the match.
	UserID      uuid.UUID    `json:"userid,omitempty"`
	SessionID   uuid.UUID    `json:"session_id,omitempty"`
	Username    string       `json:"username,omitempty"`
	ClientIP    string       `json:"client_ip,omitempty"`
	EvrID       evr.EvrID    `json:"evr_id,omitempty"`
	DiscordID   string       `json:"discord_id,omitempty"`
	//SessionFlags SessionFlags `json:"session_flags,omitempty"`
	Node    string `json:"node,omitempty"`
	session *sessionWS
}

type EndpointCompact struct {
	ExternalIP string `json:"ip,omitempty"`
	Port       string `json:"port,omitempty"`
}

func NewCompactEndpoint(endpoint evr.Endpoint) EndpointCompact {
	return EndpointCompact{
		ExternalIP: endpoint.ExternalIP.String(),
		Port:       strconv.FormatUint(uint64(endpoint.Port), 10),
	}
}

func (p BroadcasterPresence) GetUserId() string                 { return p.UserID.String() }
func (p BroadcasterPresence) GetSessionId() string              { return p.SessionID.String() }
func (p BroadcasterPresence) GetNodeId() string                 { return p.Node }
func (p BroadcasterPresence) GetHidden() bool                   { return true }
func (p BroadcasterPresence) GetPersistence() bool              { return false }
func (p BroadcasterPresence) GetUsername() string               { return p.Username }
func (p BroadcasterPresence) GetStatus() string                 { return "" }
func (p BroadcasterPresence) GetReason() runtime.PresenceReason { return runtime.PresenceReasonUnknown }
func (p BroadcasterPresence) GetEvrId() string                  { return p.EvrID.Token() }

type Broadcaster struct {
	Presence      BroadcasterPresence
	LabelString   string
	LastHeartbeat time.Time
}

type BroadcasterParams struct {
	VersionLock evr.Symbol
	GroupID     uuid.UUID
	Region      evr.Symbol
}

type BroadcasterRegistry struct {
	sync.RWMutex

	metrics Metrics
	logger  *zap.Logger

	broadcasters                           map[evr.Symbol]map[uuid.UUID]Broadcaster // map[VersionLock]map[SessionID]Broadcaster
	broadcastersByVersionByChannelByRegion map[BroadcasterParams][]Broadcaster      // map[VersionLock/Channel/Region]map[SessionID]Broadcaster
	health                                 map[uuid.UUID]time.Time

	allocated map[uuid.UUID]MatchID // map[sessionID]matchID

	//BroadcastersByParameters map[BroadcasterParams]map[uuid.UUID]*Broadcaster
}

func NewBroadcasterRegistry(logger *zap.Logger, metrics Metrics) *BroadcasterRegistry {
	return &BroadcasterRegistry{
		broadcasters: make(map[evr.Symbol]map[uuid.UUID]Broadcaster),
	}
}

func (br *BroadcasterRegistry) Add(presence BroadcasterPresence) {

	registration := Broadcaster{
		Presence:      presence,
		LastHeartbeat: time.Now(),
	}

	// If the broadcaster context closes, then remove the broadcaster from the registry.
	go func() {
		<-presence.session.Context().Done()
		br.Remove(presence.SessionID)
	}()

	br.Lock()
	defer br.Unlock()
	br.broadcasters[presence.VersionLock][presence.SessionID] = registration
}

func (br *BroadcasterRegistry) Remove(id uuid.UUID) {
	br.Lock()
	defer br.Unlock()
	for _, b := range br.broadcasters {
		if _, ok := b[id]; ok {
			delete(b, id)
		}
	}
}

func (br *BroadcasterRegistry) Range(f func(id uuid.UUID, registration Broadcaster)) {
	br.RLock()
	defer br.RUnlock()
	for _, b := range br.broadcasters {
		for id, registration := range b {
			f(id, registration)
		}
	}
}

func (br *BroadcasterRegistry) Count() int {
	br.RLock()
	defer br.RUnlock()
	count := 0
	for _, b := range br.broadcasters {
		count += len(b)
	}
	return count
}

func (br *BroadcasterRegistry) Get(id uuid.UUID) Broadcaster {
	br.RLock()
	defer br.RUnlock()
	for _, b := range br.broadcasters {
		if r, ok := b[id]; ok {
			return r
		}
	}
	return Broadcaster{}
}

func (br *BroadcasterRegistry) GetBroadcasters(versionLock evr.Symbol, includeAllocated bool) map[uuid.UUID]Broadcaster {
	br.RLock()
	defer br.RUnlock()
	result := make(map[uuid.UUID]Broadcaster)
	if b, ok := br.broadcasters[versionLock]; ok {
		for id, registration := range b {
			if includeAllocated || br.allocated[id].IsNil() {
				result[id] = registration
			}
		}
	}
	return result
}

func (br *BroadcasterRegistry) AllocateBroadcaster(versionLock evr.Symbol, groupIDs []uuid.UUID, regions []evr.Symbol, endpointPriority []string) *Broadcaster {
	br.Lock()
	defer br.Unlock()
	options := make(map[EndpointCompact]Broadcaster, 0)

	for _, groupID := range groupIDs {
		for _, region := range regions {

			p := BroadcasterParams{
				VersionLock: versionLock,
				GroupID:     groupID,
				Region:      region,
			}

			for _, b := range br.broadcastersByVersionByChannelByRegion[p] {
				c := NewCompactEndpoint(b.Presence.Endpoint)
				options[c] = b
			}

		}
	}

	for _, e := range endpointPriority {
		_ = e
		for e, registration := range options {
			if e == e {
				go func() {
					tempID := MatchID{
						uuid: uuid.Nil,
						node: "temp",
					}

					br.allocated[registration.Presence.SessionID] = tempID
					<-time.After(15 * time.Second)
					if br.allocated[registration.Presence.SessionID] == tempID {
						delete(br.allocated, registration.Presence.SessionID)
					}
				}()
				return &registration
			}
		}
	}
	return nil
}

func (br *BroadcasterRegistry) HealthCheck() {
	for {
		select {
		case <-time.After(10 * time.Second):
		}
		br.RLock()
		defer br.RUnlock()
		for _, b := range br.broadcasters {
			for _, bb := range b {
				if time.Since(bb.LastHeartbeat) > 60*time.Second {
					br.Remove(bb.Presence.SessionID)
				}
			}
		}
	}
}

type blugeBroadcaster struct {
	ID     string
	Fields map[string]interface{}
}

type BlugeBroadcasterResult struct {
	Hits []*blugeBroadcaster
}

func IterateBlugeBroadcasters(dmi search.DocumentMatchIterator, loadFields map[string]struct{}, logger *zap.Logger) (*BlugeResult, error) {
	rv := &BlugeResult{}
	dm, err := dmi.Next()
	for dm != nil && err == nil {
		var bm blugeBroadcaster
		bm.Fields = make(map[string]interface{})
		err = dm.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				bm.ID = string(value)
			}
			if _, ok := loadFields[field]; ok {
				if field == "time_step" {
					// hard-coded numeric decoding
					bm.Fields[field], err = bluge.DecodeNumericFloat64(value)
					if err != nil {
						logger.Warn("error decoding numeric value: %v", zap.Error(err))
					}
				} else {
					bm.Fields[field] = string(value)
				}
			}

			return true
		})
		if err != nil {
			return nil, fmt.Errorf("error visiting stored field: %v", err.Error())
		}
		//rv.Hits = append(rv.Hits, &bm)
		dm, err = dmi.Next()
	}
	if err != nil {
		return nil, fmt.Errorf("error iterating document matches: %v", err.Error())
	}

	return rv, nil
}

func (r *LocalMatchRegistry) ListBroadcasters(ctx context.Context, limit int, queryString string) ([]*Broadcaster, error) {
	if limit == 0 {
		return make([]*Broadcaster, 0), nil
	}

	indexReader, err := r.indexWriter.Reader()
	if err != nil {
		return nil, fmt.Errorf("error accessing index reader: %v", err.Error())
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			r.logger.Error("error closing index reader", zap.Error(err))
		}
	}()

	var labelResults *BlugeResult

	// Apply the query filter to the set of known match labels.
	var q bluge.Query
	if queryString == "" {
		q = bluge.NewMatchAllQuery()
	} else {
		parsed, err := ParseQueryString(queryString)
		if err != nil {
			return nil, fmt.Errorf("error parsing query string: %v", err.Error())
		}
		q = parsed
	}

	searchReq := bluge.NewTopNSearch(limit, q)
	searchReq.SortBy([]string{"-_score", "-create_time"})

	labelResultsItr, err := indexReader.Search(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("error listing matches by query: %v", err.Error())
	}
	labelResults, err = IterateBlugeMatches(labelResultsItr,
		map[string]struct{}{
			"label_string": {},
		}, r.logger)
	if err != nil {
		return nil, fmt.Errorf("error iterating bluge matches: %v", err.Error())
	}

	// Results.
	results := make([]*Broadcaster, 0, limit)

	// Use any eligible authoritative matches first.
	if labelResults != nil {
		for _, hit := range labelResults.Hits {

			var labelString string
			if l, ok := hit.Fields["label_string"]; ok {
				if labelString, ok = l.(string); !ok {
					r.logger.Warn("Field not a string in match registry label cache: label_string")
					continue
				}
			} else {
				r.logger.Warn("Field not found in match registry label cache: label_string")
				continue
			}

			results = append(results, &Broadcaster{
				LabelString: labelString,
			})
			if len(results) == limit {
				return results, nil
			}
		}
	}

	return results, nil
}
