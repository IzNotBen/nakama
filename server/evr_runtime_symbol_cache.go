package server

import (
	"context"
	"encoding/binary"

	"github.com/go-redis/redis"
	"github.com/heroiclabs/nakama/v3/server/evr"
)

type SymbolCache struct {
	ctx         context.Context
	ctxCancelFn context.CancelFunc
	client      *redis.Client
}

func NewSymbolCache() *SymbolCache {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &SymbolCache{
		ctx:         ctx,
		ctxCancelFn: cancelFn,
	}
}

func (s *SymbolCache) Connect(url string) {
	// redis://<user>:<pass>@localhost:6379/<db>
	opt, err := redis.ParseURL(url)
	if err != nil {
		panic(err)
	}

	s.client = redis.NewClient(opt)
}

func (s *SymbolCache) Close() {
	s.client.Close()
}

func (s *SymbolCache) LoadCoreSymbols() {
	symbols := make(map[uint64]string, len(evr.CoreSymbols))
	for hash, sym := range evr.CoreSymbols {
		symbols[hash] = string(sym)
	}
	s.LoadSymbols(symbols)

}

func (s *SymbolCache) key(sym uint64) string {
	b := append([]byte("sym:"), make([]byte, 8)...)
	binary.LittleEndian.PutUint64(b[4:], sym)
	return string(b)
}

func (s *SymbolCache) LoadSymbols(symbols map[uint64]string) {
	for sym, str := range symbols {
		s.Set(sym, str)
	}
}

func (s *SymbolCache) GetSymbol(sym uint64) (string, error) {
	b := append([]byte("sym:"), make([]byte, 8)...)
	binary.LittleEndian.PutUint64(b[4:], sym)
	return s.client.Get(string(b)).Result()
}

func (s *SymbolCache) Set(sym uint64, str string) error {
	return s.client.Set(s.key(sym), str, 0).Err()
}

func (s *SymbolCache) Get(sym uint64) string {
	return s.client.Get(s.key(sym)).String()
}
