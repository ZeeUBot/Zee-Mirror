package service

import (
	"context"
	"fmt"
	"strings"

	"zee-mirror/internal/domain"
)

type StorageProvider struct {
	Name string
	Type string
	Icon string
}

func (s *BotService) GetAvailableStorages() ([]StorageProvider, error) {
	cmd := execCommand(context.Background(), "rclone", "listremotes")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to list rclone remotes: %v", domain.ErrExternal, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var providers []StorageProvider
	for _, line := range lines {
		name := strings.TrimSuffix(line, ":")
		if name == "" {
			continue
		}

		providers = append(providers, StorageProvider{
			Name: name,
			Type: "rclone",
			Icon: "📁",
		})
	}

	return providers, nil
}
