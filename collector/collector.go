package collector

import (
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/librato/snap-plugin-lib-go/v2/plugin"
	"github.com/papertrail/go-tail/follower"
)

type garbageCollectionCollector struct{}

type metricInfo struct {
	name        string
	structField string
	unit        string
	description string
}

type logStateContainer struct {
	mu    sync.RWMutex
	count int64
	lines []string
}

func (lsc *logStateContainer) addLine(line string) {
	lsc.mu.Lock()
	defer lsc.mu.Unlock()
	lsc.lines = append(lsc.lines, line)
	lsc.count++
}

func (lsc *logStateContainer) collectAndReset() (int64, []string) {
	lsc.mu.Lock()
	defer lsc.mu.Unlock()

	count := lsc.count
	lines := lsc.lines

	lsc.count = 0
	lsc.lines = []string{}

	return count, lines
}

var logState logStateContainer
var pauseMetricList []metricInfo
var fullGCMetricList []metricInfo

type statisticStruct interface {
	getFieldByString() float64
}

type javaG1FullGCStatistics struct {
	TotalTime           float64
	TimeUser            float64
	TimeSys             float64
	TimeReal            float64
	EdenUsageBefore     float64
	EdenUsageAfter      float64
	EdenCapacityBefore  float64
	EdenCapacityAfter   float64
	HeapUsageBefore     float64
	HeapUsageAfter      float64
	HeapCapacityBefore  float64
	HeapCapacityAfter   float64
	SurvivorUsageBefore float64
	SurvivorUsageAfter  float64
	MetaspaceBefore     float64
	MetaspaceAfter      float64
}

func (j *javaG1FullGCStatistics) getFieldByString(name string) float64 {
	r := reflect.ValueOf(j)
	f := reflect.Indirect(r).FieldByName(name)
	return float64(f.Float())
}

type javaG1EventStatistics struct {
	EvacuationType         string
	ToSpaceExhausted       bool
	InitialMark            bool
	HumongousAllocation    bool
	TotalTime              float64
	TimeUser               float64
	TimeSys                float64
	TimeReal               float64
	EdenUsageBefore        float64
	EdenUsageAfter         float64
	EdenCapacityBefore     float64
	EdenCapacityAfter      float64
	HeapUsageBefore        float64
	HeapUsageAfter         float64
	HeapCapacityBefore     float64
	HeapCapacityAfter      float64
	SurvivorUsageBefore    float64
	SurvivorUsageAfter     float64
	ParallelProcessingTime float64
	GCWorkerCount          float64
	GCWorkerStartDiff      float64
	ExtRootScanTime        float64
	CodeRootScanTime       float64
	ObjectCopyTime         float64
	TerminationTime        float64
	TerminationAttempts    float64
	GCWorkerOtherTime      float64
	GCWorkerTotalTime      float64
	GCWorkerEndDiff        float64
	CodeRootFixupTime      float64
	CodeRootPurgeTime      float64
	ClearCTTime            float64
	OtherTime              float64
	ChooseCSetTime         float64
	RefProcTime            float64
	RefEnqTime             float64
	RedirtyCardTime        float64
	HumongousRegisterTime  float64
	HumongousReclaimTime   float64
	FreeCSetTime           float64
}

func (j *javaG1EventStatistics) getFieldByString(name string) float64 {
	r := reflect.ValueOf(j)
	f := reflect.Indirect(r).FieldByName(name)
	return float64(f.Float())
}

type gauge struct {
	min   float64
	max   float64
	sum   float64
	count int64
}

func (g *gauge) value() float64 {
	return g.sum / float64(g.count)
}

func (g *gauge) combine(newgauge gauge) error {
	if newgauge.min < g.min || g.count == 0 {
		g.min = newgauge.min
	}
	if newgauge.max > g.max || g.count == 0 {
		g.max = newgauge.max
	}
	g.sum += newgauge.sum
	g.count += newgauge.count
	return nil
}

func (g *gauge) addValue(newval float64) error {
	if newval < g.min || g.count == 0 {
		g.min = newval
	}
	if newval > g.max || g.count == 0 {
		g.max = newval
	}
	g.sum += newval
	g.count++
	return nil

}
func New() plugin.Collector {
	return &garbageCollectionCollector{}
}

func BuildMetricInfo() error {
	//create the metric lists
	pauseMetricList = []metricInfo{
		metricInfo{"pause", "TotalTime", "ms", "evacuation pauses"},
		metricInfo{"pause/gc/workers", "GCWorkerCount", "", "gc worker count"},
		metricInfo{"pause/time/user", "TimeUser", "s", "cpu time in user code"},
		metricInfo{"pause/time/sys", "TimeSys", "s", "cpu time in kernel"},
		metricInfo{"pause/time/real", "TimeReal", "s", "wall clock cpu time"},
		metricInfo{"pause/eden/before", "EdenUsageBefore", "bytes", "bytes in the eden space before collection"},
		metricInfo{"pause/eden/after", "EdenUsageAfter", "bytes", "bytes in the eden space after collection"},
		//metricInfo{"", "EdenCapacityBefore", "", ""},
		//metricInfo{"", "EdenCapacityAfter", "", ""}, //capacity metrics not that useful
		metricInfo{"pause/heap/before", "HeapUsageBefore", "bytes", "bytes in the heap after collection"},
		metricInfo{"pause/heap/after", "HeapUsageAfter", "bytes", "bytes in the heap after collection"},
		//metricInfo{"", "HeapCapacityBefore", "", ""},
		//metricInfo{"", "HeapCapacityAfter", "", ""},
		metricInfo{"pause/survivor/before", "SurvivorUsageBefore", "bytes", "bytes in the survivor space before collection"},
		metricInfo{"pause/survivor/after", "SurvivorUsageAfter", "bytes", "bytes in the survivor space after collection"},
		//metricInfo{"", "ParallelProcessingTime", "", ""}, //can be inferred by summing parallel processes
		metricInfo{"pause/gc/workers/start_diff", "GCWorkerStartDiff", "ms", "worker start time difference"},
		metricInfo{"pause/ext_root_scan_time", "ExtRootScanTime", "ms", "external root scanning"},
		metricInfo{"pause/code_root_scan_time", "CodeRootScanTime", "ms", "code root scanning"},
		metricInfo{"pause/object_copy_time", "ObjectCopyTime", "ms", "object copy"},
		metricInfo{"pause/termination_time", "TerminationTime", "ms", "termination"},
		metricInfo{"pause/termination/attemps", "TerminationAttempts", "", "number of termination attempts"},
		metricInfo{"pause/gc/workers/other_time", "GCWorkerOtherTime", "ms", "gc workers other small tasks"},
		metricInfo{"pause/gc/workers/total_time", "GCWorkerTotalTime", "ms", "gc workers"},
		metricInfo{"pause/gc/workers/end_diff", "GCWorkerEndDiff", "ms", "gc workers end diff"},
		metricInfo{"pause/code_root_fixup_time", "CodeRootFixupTime", "ms", "code root fixup"},
		metricInfo{"pause/code_root_purge_time", "CodeRootPurgeTime", "ms", "code root purge"},
		metricInfo{"pause/clear_ct_time", "ClearCTTime", "ms", "clear ct"},
		metricInfo{"pause/other_time", "OtherTime", "ms", "other small tasks"},
		metricInfo{"pause/choose_cset_time", "ChooseCSetTime", "ms", "choose cset"},
		metricInfo{"pause/ref_proc_time", "RefProcTime", "ms", "reference processing"},
		metricInfo{"pause/ref_enq_time", "RefEnqTime", "ms", "reference enqueue"},
		metricInfo{"pause/redirty_card_time", "RedirtyCardTime", "ms", "redirty card"},
		metricInfo{"pause/hummongous_register_time", "HumongousRegisterTime", "ms", "humongous register"},
		metricInfo{"pause/humongous_reclaim_time", "HumongousReclaimTime", "ms", "humongous reclaim"},
		metricInfo{"pause/free_cset_time", "FreeCSetTime", "ms", "free cset"},
	}

	fullGCMetricList = []metricInfo{
		metricInfo{"full", "TotalTime", "ms", "evacuation pauses"},
		metricInfo{"full/time/user", "TimeUser", "s", "cpu time in user code"},
		metricInfo{"full/time/sys", "TimeSys", "s", "cpu time in kernel"},
		metricInfo{"full/time/real", "TimeReal", "s", "wall clock cpu time"},
		metricInfo{"full/eden/before", "EdenUsageBefore", "bytes", "bytes in the eden space before collection"},
		metricInfo{"full/eden/after", "EdenUsageAfter", "bytes", "bytes in the eden space after collection"},
		metricInfo{"full/heap/before", "HeapUsageBefore", "bytes", "bytes in the heap after collection"},
		metricInfo{"full/heap/after", "HeapUsageAfter", "bytes", "bytes in the heap after collection"},
		metricInfo{"full/survivor/before", "SurvivorUsageBefore", "bytes", "bytes in the survivor space before collection"},
		metricInfo{"full/survivor/after", "SurvivorUsageAfter", "bytes", "bytes in the survivor space after collection"},
		metricInfo{"full/metaspace/before", "MetaspaceBefore", "bytes", "bytes in the survivor space before collection"},
		metricInfo{"full/metaspace/after", "MetaspaceAfter", "bytes", "bytes in the survivor space after collection"},
	}

	return nil

}

func (g *garbageCollectionCollector) PluginDefinition(def plugin.CollectorDefinition) error {

	BuildMetricInfo()

	for _, ml := range [][]metricInfo{fullGCMetricList, pauseMetricList} {
		for _, v := range ml {
			def.DefineMetric("/garbage_collector/java/g1/"+v.name+"/mean", v.unit, true, "The mean time for "+v.description)
			def.DefineMetric("/garbage_collector/java/g1/"+v.name+"/count", v.unit, true, "The count of "+v.description)
			def.DefineMetric("/garbage_collector/java/g1/"+v.name+"/max", v.unit, true, "The max time for "+v.description)
			def.DefineMetric("/garbage_collector/java/g1/"+v.name+"/min", v.unit, true, "The min time for "+v.description)
		}
	}
	return nil
}

func (g *garbageCollectionCollector) Load(ctx plugin.Context) error {
	err := handleConfig(ctx)
	if err != nil {
		return err
	}

	logState = logStateContainer{}

	cfg := getConfig(ctx)
	for _, v := range cfg.Groups {
		if v.Type == "java_g1" {
			for _, l := range v.Locations {
				go tailJavaG1Log(l)
			}
		}
	}

	return nil
}

func (g *garbageCollectionCollector) Collect(ctx plugin.CollectContext) error {

	count, lines := logState.collectAndReset()
	if count > 0 {
		_ = g.processJavaG1(ctx, lines)
	}

	return nil
}

func (g *garbageCollectionCollector) processJavaG1(ctx plugin.CollectContext, lines []string) error {
	//regex
	findZuluTime, _ := regexp.Compile("\\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[1-2]\\d|3[0-1])T(?:[0-1]\\d|2[0-3]):[0-5]\\d:[0-5]\\d(?:\\.\\d+|)(?:Z|(?:\\+|\\-)(?:\\d{2}):?(?:\\d{2}))")

	//initialize buckets
	var statsAccumulater []javaG1EventStatistics
	var statsFullGCAccumulater []javaG1FullGCStatistics

	//process chunks
	chunkStarted := false
	chunkData := []string{}
	chunkType := ""
	for _, line := range lines {
		if findZuluTime.MatchString(line) {
			if chunkStarted {
				if chunkType == "pause" {
					j, err := parseJavaG1Evacuation(chunkData)
					if err == nil {
						statsAccumulater = append(statsAccumulater, j)
					} else {
						fmt.Println(err)
					}
				} else if chunkType == "full" {
					j, err := parseJavaG1FullGC(chunkData)
					if err == nil {
						statsFullGCAccumulater = append(statsFullGCAccumulater, j)
					} else {
						fmt.Println(err)
					}
				}
				chunkData = []string{}
				chunkType = getChunkType(line)

			} else {
				//get the type
				chunkType = getChunkType(line)
				chunkStarted = true
			}
		}
		chunkData = append(chunkData, line)
	}
	//aggregate data
	fullMetrics := map[string]map[string]gauge{}
	for _, v := range fullGCMetricList {
		fullMetrics[v.name] = map[string]gauge{}
	}
	for _, v := range statsFullGCAccumulater {
		tagset := ""
		for _, metric := range fullGCMetricList {
			if gaugeval, ok := fullMetrics[metric.name][tagset]; ok {
				_ = gaugeval.addValue(v.getFieldByString(metric.structField))
				fullMetrics[metric.name][tagset] = gaugeval
			} else {
				gaugeval := gauge{}
				_ = gaugeval.addValue(v.getFieldByString(metric.structField))
				fullMetrics[metric.name][tagset] = gaugeval
			}

		}
	}

	pauseMetrics := map[string]map[string]gauge{}
	for _, v := range pauseMetricList {
		pauseMetrics[v.name] = map[string]gauge{}
	}
	for _, v := range statsAccumulater {
		tagset := v.EvacuationType + "|" + strconv.FormatBool(v.InitialMark) + "|" + strconv.FormatBool(v.HumongousAllocation) + "|" + strconv.FormatBool(v.ToSpaceExhausted)
		for _, metric := range pauseMetricList {
			if gaugeval, ok := pauseMetrics[metric.name][tagset]; ok {
				_ = gaugeval.addValue(v.getFieldByString(metric.structField))
				pauseMetrics[metric.name][tagset] = gaugeval
			} else {
				gaugeval := gauge{}
				_ = gaugeval.addValue(v.getFieldByString(metric.structField))
				pauseMetrics[metric.name][tagset] = gaugeval
			}

		}
	}

	//send metrics
	for _, ml := range []map[string]map[string]gauge{fullMetrics, pauseMetrics} {
		for metricName, metricInfo := range ml {
			for tagset, val := range metricInfo {
				tags := parseTagSet(tagset)
				_ = ctx.AddMetric("/garbage_collector/java/g1/"+metricName+"/mean", val.value(), tags)
				_ = ctx.AddMetric("/garbage_collector/java/g1/"+metricName+"/count", val.count, tags)
				_ = ctx.AddMetric("/garbage_collector/java/g1/"+metricName+"/min", val.min, tags)
				_ = ctx.AddMetric("/garbage_collector/java/g1/"+metricName+"/max", val.max, tags)
			}
		}
	}

	return nil
}

func parseTagSet(tagset string) plugin.MetricModifier {
	tags := map[string]string{}
	values := strings.Split(tagset, "|")
	if len(values) == 4 {
		tags["evacuation_type"] = values[0]
		tags["inital_mark"] = values[1]
		tags["humongous_allocation"] = values[2]
		tags["to_space_exhausted"] = values[3]
	}

	return plugin.MetricTags(tags)

}

func getChunkType(line string) string {
	if strings.Contains(line, "[GC pause (") {
		return "pause"
	} else if strings.Contains(line, "[Full GC (") {
		return "full"
	}
	return "unknown"

}

func parseJavaG1FullGC(text []string) (javaG1FullGCStatistics, error) {
	stats := javaG1FullGCStatistics{}
	findValues, _ := regexp.Compile("[-+]?([0-9]*\\.[0-9]+|[0-9]+)")
	findValuesWithUnit, _ := regexp.Compile("[-+]?([0-9]*\\.[0-9]+\\w|[0-9]+\\w)")

	for _, v := range text {
		if strings.Contains(v, "[Full GC (") {
			found := findValues.FindAllString(v, -1)
			stats.TotalTime, _ = strconv.ParseFloat(found[len(found)-1], 64)
		} else if strings.Contains(v, "[Times: user=") {
			found := findValues.FindAllString(v, -1)
			stats.TimeUser, _ = strconv.ParseFloat(found[0], 64)
			stats.TimeSys, _ = strconv.ParseFloat(found[1], 64)
			stats.TimeReal, _ = strconv.ParseFloat(found[2], 64)
		} else if strings.Contains(v, "[Eden: ") {
			found := findValuesWithUnit.FindAllString(v, -1)
			stats.EdenUsageBefore = convertUnitToBytes(found[0])
			stats.EdenCapacityBefore = convertUnitToBytes(found[1])
			stats.EdenUsageAfter = convertUnitToBytes(found[2])
			stats.EdenCapacityAfter = convertUnitToBytes(found[3])
			stats.SurvivorUsageBefore = convertUnitToBytes(found[4])
			stats.SurvivorUsageAfter = convertUnitToBytes(found[5])
			stats.HeapUsageBefore = convertUnitToBytes(found[6])
			stats.HeapCapacityBefore = convertUnitToBytes(found[7])
			stats.HeapUsageAfter = convertUnitToBytes(found[8])
			stats.HeapCapacityAfter = convertUnitToBytes(found[9])
			stats.MetaspaceBefore = convertUnitToBytes(found[10])
			stats.MetaspaceAfter = convertUnitToBytes(found[11])
		}
	}
	return stats, nil
}

func parseJavaG1Evacuation(text []string) (javaG1EventStatistics, error) {
	stats := javaG1EventStatistics{}
	findValues, _ := regexp.Compile("[-+]?([0-9]*\\.[0-9]+|[0-9]+)")
	findValuesWithUnit, _ := regexp.Compile("[-+]?([0-9]*\\.[0-9]+\\w|[0-9]+\\w)")
	for _, v := range text {
		if strings.Contains(v, "[GC pause (") {
			if strings.Contains(v, "(young)") {
				stats.EvacuationType = "young"
			} else if strings.Contains(v, "(mixed)") {
				stats.EvacuationType = "mixed"
			} else {
				stats.EvacuationType = "unknown"
			}
			if strings.Contains(v, "(initial-mark)") {
				stats.InitialMark = true
			}
			if strings.Contains(v, "(G1 Humongous Allocation)") {
				stats.HumongousAllocation = true
			}
			if strings.Contains(v, "(to-space exhausted)") {
				stats.ToSpaceExhausted = true
			}
			if s, err := strconv.ParseFloat(strings.Split(strings.Split(v, ",")[1], " ")[1], 64); err == nil {
				stats.TotalTime = s
			} else {
				return stats, err
			}
		} else if strings.Contains(v, "[Times: user=") {
			found := findValues.FindAllString(v, -1)
			stats.TimeUser, _ = strconv.ParseFloat(found[0], 64)
			stats.TimeSys, _ = strconv.ParseFloat(found[1], 64)
			stats.TimeReal, _ = strconv.ParseFloat(found[2], 64)
		} else if strings.Contains(v, "[Eden: ") {
			found := findValuesWithUnit.FindAllString(v, -1)
			stats.EdenUsageBefore = convertUnitToBytes(found[0])
			stats.EdenCapacityBefore = convertUnitToBytes(found[1])
			stats.EdenUsageAfter = convertUnitToBytes(found[2])
			stats.EdenCapacityAfter = convertUnitToBytes(found[3])
			stats.SurvivorUsageBefore = convertUnitToBytes(found[4])
			stats.SurvivorUsageAfter = convertUnitToBytes(found[5])
			stats.HeapUsageBefore = convertUnitToBytes(found[6])
			stats.HeapCapacityBefore = convertUnitToBytes(found[7])
			stats.HeapUsageAfter = convertUnitToBytes(found[8])
			stats.HeapCapacityAfter = convertUnitToBytes(found[9])
		} else if strings.Contains(v, "[Parallel Time: ") {
			found := findValues.FindAllString(v, -1)
			stats.ParallelProcessingTime, _ = strconv.ParseFloat(found[0], 64)
			stats.GCWorkerCount, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[GC Worker Start ") {
			found := findValues.FindAllString(v, -1)
			stats.GCWorkerStartDiff, _ = strconv.ParseFloat(found[3], 64)
		} else if strings.Contains(v, "[Ext Root Scanning") {
			found := findValues.FindAllString(v, -1)
			stats.ExtRootScanTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[Code Root Scanning") {
			found := findValues.FindAllString(v, -1)
			stats.CodeRootScanTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[Object Copy") {
			found := findValues.FindAllString(v, -1)
			stats.ObjectCopyTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[Termination (") {
			found := findValues.FindAllString(v, -1)
			stats.TerminationTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[Termination Attempts:") {
			found := findValues.FindAllString(v, -1)
			stats.TerminationAttempts, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[GC Worker Other ") {
			found := findValues.FindAllString(v, -1)
			stats.GCWorkerOtherTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[GC Worker Total") {
			found := findValues.FindAllString(v, -1)
			stats.GCWorkerTotalTime, _ = strconv.ParseFloat(found[1], 64)
		} else if strings.Contains(v, "[GC Worker End") {
			found := findValues.FindAllString(v, -1)
			stats.GCWorkerEndDiff, _ = strconv.ParseFloat(found[3], 64)
		} else if strings.Contains(v, "[Code Root Fixup:") {
			found := findValues.FindAllString(v, -1)
			stats.CodeRootFixupTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Code Root Purge:") {
			found := findValues.FindAllString(v, -1)
			stats.CodeRootPurgeTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Clear CT:") {
			found := findValues.FindAllString(v, -1)
			stats.ClearCTTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Other: ") {
			found := findValues.FindAllString(v, -1)
			stats.OtherTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Choose CSet") {
			found := findValues.FindAllString(v, -1)
			stats.ChooseCSetTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Ref Proc:") {
			found := findValues.FindAllString(v, -1)
			stats.RefProcTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "Ref Enq:") {
			found := findValues.FindAllString(v, -1)
			stats.RefEnqTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Redirty Cards:") {
			found := findValues.FindAllString(v, -1)
			stats.RedirtyCardTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "Humongous Register") {
			found := findValues.FindAllString(v, -1)
			stats.HumongousRegisterTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Humongous Reclaim:") {
			found := findValues.FindAllString(v, -1)
			stats.HumongousReclaimTime, _ = strconv.ParseFloat(found[0], 64)
		} else if strings.Contains(v, "[Free CSet:") {
			found := findValues.FindAllString(v, -1)
			stats.FreeCSetTime, _ = strconv.ParseFloat(found[0], 64)
		}
	}

	return stats, nil
}

func convertUnitToBytes(val string) float64 {
	fl, _ := strconv.ParseFloat(val[0:len(val)-1], 64)
	unit := val[len(val)-1]
	switch unit {
	case 'M':
		fl *= 1024 * 1024
	case 'G':
		fl *= 1024 * 1024 * 1024
	case 'K':
		fl *= 1024
	}
	return fl
}

func getConfig(ctx plugin.Context) config {
	obj, ok := ctx.Load("config")
	if !ok {
		return defaultConfig()
	}
	return *(obj.(*config))
}

func addLogLine(line string) error {
	logState.addLine(line)
	return nil
}

func tailJavaG1Log(location string) error {
	t, err := follower.New(location, follower.Config{
		Whence: io.SeekEnd,
		Offset: 0,
		Reopen: true,
	})
	if err != nil {
		return err
	}
	for line := range t.Lines() {
		addLogLine(line.String())
	}

	if t.Err() != nil {
		fmt.Println(t.Err())
		return t.Err()
	}

	return nil
}
