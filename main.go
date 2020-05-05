package main

import (
	"./collector"

	"github.com/librato/snap-plugin-lib-go/v2/runner"
)

const pluginName = "garbage-collection"
const pluginVersion = "0.0.1"

func main() {
	runner.StartCollector(collector.New(), pluginName, pluginVersion)
}
