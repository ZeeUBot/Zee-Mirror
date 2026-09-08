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

// URLMatcher is an opt-in capability seam: engines that recognize specific
// URL shapes implement it so callers route by capability instead of
// scattering strings.Contains checks. Engines without it (e.g. aria2 as the
// default) stay untouched.
type URLMatcher interface {
	CanHandle(url string) bool
}

// urlRoutedEngines is tried in order by SuggestDownloadEngine.
var urlRoutedEngines = []string{"telegram", "mega", "drive"}

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

// SuggestDownloadEngine returns the first registered engine claiming the URL.
// Unknown URLs yield ok=false and the caller falls back to its default engine.
func SuggestDownloadEngine(url string, cfg *config.Config) (engine downloader.DownloadEngine, name string, ok bool) {
	for _, name := range urlRoutedEngines {
		engine, err := CreateDownloadEngine(name, cfg)
		if err != nil {
			continue
		}
		matcher, implements := engine.(URLMatcher)
		if !implements || !matcher.CanHandle(url) {
			continue
		}
		return engine, name, true
	}
	return nil, "", false
}
