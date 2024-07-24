// Copyright 2021 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/heroiclabs/nakama/v3/server/evr"
)

type LocalProfileCache struct {
	sync.RWMutex

	tracker Tracker

	profileExpirySec int64

	ctx         context.Context
	ctxCancelFn context.CancelFunc

	// Fast lookup of profiles for players already in matches
	cache map[evr.EvrID]string // Mode: StreamModeEvr, Subject: matchID, Subcontext: evrID.UUID() -> profile JSON
}

func NewLocalProfileCache(tracker Tracker, profileExpirySec int64) *LocalProfileCache {
	ctx, ctxCancelFn := context.WithCancel(context.Background())

	s := &LocalProfileCache{
		ctx:         ctx,
		ctxCancelFn: ctxCancelFn,

		profileExpirySec: profileExpirySec,

		cache:   make(map[evr.EvrID]string),
		tracker: tracker,
	}

	go func() {
		ticker := time.NewTicker(2 * time.Duration(profileExpirySec) * time.Second)
		for {
			select {
			case <-s.ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				s.Lock()
				for evrID := range s.cache {
					if tracker.CountByStream(PresenceStream{
						Mode:    StreamModeService,
						Subject: evrID.UUID(),
						Label:   StreamLabelMatchService,
					}) == 0 {
						delete(s.cache, evrID)
					}
				}
				s.Unlock()
			}
		}
	}()

	return s
}

func (s *LocalProfileCache) Stop() {
	s.ctxCancelFn()
}

func (s *LocalProfileCache) IsValidProfile(matchID MatchID, evrID evr.EvrID) bool {
	s.RLock()
	defer s.RUnlock()
	return s.cache[evrID] != ""
}

func (s *LocalProfileCache) Add(matchID MatchID, evrID evr.EvrID, profile evr.ServerProfile) error {
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	s.Lock()
	s.cache[evrID] = string(data)
	s.Unlock()
	return nil
}

func (s *LocalProfileCache) Remove(matchID MatchID, evrID evr.EvrID) {
	s.Lock()
	delete(s.cache, evrID)
	s.Unlock()
}

func (s *LocalProfileCache) GetByEvrID(evrID evr.EvrID) (data string, found bool) {
	s.RLock()
	defer s.RUnlock()
	if profile, ok := s.cache[evrID]; ok {
		return profile, true
	}
	return "", false
}
