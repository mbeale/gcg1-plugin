package collector

import (
	"encoding/json"
	"fmt"

	"github.com/librato/snap-plugin-lib-go/v2/plugin"
)

type config struct {
	Groups []configGroups
}

type configGroups struct {
	Type      string
	Locations []string
}

func defaultConfig() config {
	return config{
		Groups: make([]configGroups, 0),
	}
}

func handleConfig(ctx plugin.Context) error {

	cfg := defaultConfig()

	err := json.Unmarshal(ctx.RawConfig(), &cfg)

	if err != nil {
		return fmt.Errorf("invalid config: %v", err)
	}
	// should verify values for types and that file exists

	ctx.Store("config", &cfg)
	return nil
}
