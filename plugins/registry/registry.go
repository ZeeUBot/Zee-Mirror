package registry

import (
	"fmt"
	"strings"
	"sync"
	"zee-mirror/internal/config"
	"zee-mirror/internal/downloader"
)

type DownloadEngineFactory func(cfg *config.Config) downloader.DownloadEngine
type MediaDownloaderFactory func(cfg *config.Config) downloader.MediaDownloader

var (
	downloadEngineFactories  = make(map[string]DownloadEngineFactory)
	mediaDownloaderFactories = make(map[string]MediaDownloaderFactory)
	mu                       sync.RWMutex
)

func RegisterDownloadEngine(name string, factory DownloadEngineFactory) {
	mu.Lock()
	defer mu.Unlock()
	downloadEngineFactories[strings.ToLower(name)] = factory
}

func RegisterMediaDownloader(name string, factory MediaDownloaderFactory) {
	mu.Lock()
	defer mu.Unlock()
	mediaDownloaderFactories[strings.ToLower(name)] = factory
}

func CreateDownloadEngine(name string, cfg *config.Config) (downloader.DownloadEngine, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := downloadEngineFactories[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("download engine '%s' not found", name)
	}
	return factory(cfg), nil
}

func CreateMediaDownloader(name string, cfg *config.Config) (downloader.MediaDownloader, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := mediaDownloaderFactories[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("media downloader '%s' not found", name)
	}
	return factory(cfg), nil
}
