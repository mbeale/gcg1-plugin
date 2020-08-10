package collector

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	pluginMock "github.com/librato/snap-plugin-lib-go/v2/mock"
	"github.com/stretchr/testify/mock"
)

func TestConvertUnitToBytes(t *testing.T) {
	testCases := []string{"27.0M", "27.0G", "27.0K"}
	expectedValues := []float64{27 * 1024 * 1024, 27 * 1024 * 1024 * 1024, 27 * 1024}
	for k, v := range testCases {
		if convertUnitToBytes(v) != expectedValues[k] {
			t.Fatal(fmt.Sprintf("Failed conversion %s expected %f actual %f", v, expectedValues[k], convertUnitToBytes(v)))
		}
	}
}

func TestFullSample(t *testing.T) {
	ctx := &pluginMock.Context{}
	g := New()
	f, err := os.Open("../test/full_sample.txt")
	defer f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadString('\n')
		if err == io.EOF {
			addLogLine(line)
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		addLogLine(line)
	}
	pluginConfig := &config{
		Groups: []configGroups{
			configGroups{
				Type:      "java_g1",
				Locations: []string{"/workspaces/garbage-collector/gc.log"},
			},
		},
	}
	ctx.On("Load", "config").
		Maybe().Return(pluginConfig, true)
	ctx.On("AddMetric", "/garbage_collector/java/g1/full/mean", 0.06510247499999999, mock.Anything).
		Once().Return(nil)
	ctx.On("AddMetric", "/garbage_collector/java/g1/full/count", int64(4), mock.Anything).
		Once().Return(nil)
	ctx.On("AddMetric", "/garbage_collector/java/g1/full/max", 0.1226778, mock.Anything).
		Once().Return(nil)
	ctx.On("AddMetric", "/garbage_collector/java/g1/full/min", 0.0422456, mock.Anything).
		Once().Return(nil)
	ctx.On("AddMetric", mock.Anything, mock.Anything, mock.Anything).Maybe().Return(nil)
	BuildMetricInfo()
	g.Collect(ctx)
	ctx.AssertExpectations(t)
}

func TestParseJavaG1FullGC(t *testing.T) {
	logSample := `
	2020-04-30T13:58:37.388+0500: 122.253: [Full GC (Allocation Failure)  5118M->5118M(8192M), 0.1118378 secs]
		[Eden: 10.0B(408.0M)->110.0B(418.0M) Survivors: 20.0B->220.0B Heap: 5118.9M(8194.0M)->5118.1M(8192.0M)], [Metaspace: 2649K->264K(1056768K)]
		[Times: user=0.06 sys=0.02, real=0.12 secs]`

	var sample []string
	for _, v := range strings.Split(logSample, "\n") {
		sample = append(sample, v)
	}

	compare := javaG1FullGCStatistics{
		TotalTime:           0.1118378,
		TimeUser:            0.06,
		TimeSys:             0.02,
		TimeReal:            0.12,
		EdenUsageBefore:     10,
		EdenUsageAfter:      110,
		EdenCapacityBefore:  408 * 1024 * 1024,
		EdenCapacityAfter:   418 * 1024 * 1024,
		HeapUsageBefore:     5118.9 * 1024 * 1024,
		HeapUsageAfter:      5118.1 * 1024 * 1024,
		HeapCapacityBefore:  8194 * 1024 * 1024,
		HeapCapacityAfter:   8192 * 1024 * 1024,
		SurvivorUsageBefore: 20,
		SurvivorUsageAfter:  220,
		MetaspaceBefore:     2649 * 1024,
		MetaspaceAfter:      264 * 1024,
	}

	if stats, err := parseJavaG1FullGC(sample); err == nil {
		if !reflect.DeepEqual(compare, stats) {
			reflectcompare := reflect.ValueOf(compare)
			reflectstats := reflect.ValueOf(stats)
			for i, n := 0, reflectcompare.NumField(); i < n; i++ {
				field1 := reflectcompare.Field(i)
				field2 := reflectstats.Field(i)
				if field1.Interface() != field2.Interface() {
					reflectcompare := reflect.ValueOf(&compare)
					elementsCompare := reflectcompare.Elem()
					t.Fatal(fmt.Sprintf("For field %s expected %s actual %s ", elementsCompare.Type().Field(i).Name, field1, field2))
				}
			}
		}

	} else {
		t.Fatal(err)
	}
}

func TestParseJavaG1YoungEvacuation(t *testing.T) {
	logSample := `2020-04-22T10:32:49.777+0500: 40.222: [GC pause (G1 Evacuation Pause) (young), 0.0038162 secs]
	[Parallel Time: 2.8 ms, GC Workers: 4]
	   [GC Worker Start (ms): Min: 40222.7, Avg: 40222.7, Max: 40222.8, Diff: 0.1]
	   [Ext Root Scanning (ms): Min: 0.2, Avg: 0.7, Max: 2.0, Diff: 1.8, Sum: 2.6]
	   [Update RS (ms): Min: 0.0, Avg: 0.0, Max: 0.0, Diff: 0.0, Sum: 0.0]
		  [Processed Buffers: Min: 0, Avg: 0.0, Max: 0, Diff: 0, Sum: 0]        
	   [Scan RS (ms): Min: 0.0, Avg: 0.0, Max: 0.0, Diff: 0.0, Sum: 0.0]
	   [Code Root Scanning (ms): Min: 0.0, Avg: 0.9, Max: 0.1, Diff: 0.1, Sum: 0.1]
	   [Object Copy (ms): Min: 0.6, Avg: 1.8, Max: 2.4, Diff: 1.8, Sum: 7.2]    
	   [Termination (ms): Min: 0.0, Avg: 0.1, Max: 0.1, Diff: 0.1, Sum: 0.3]
		  [Termination Attempts: Min: 1, Avg: 1.5, Max: 2, Diff: 1, Sum: 6]
	   [GC Worker Other (ms): Min: 0.0, Avg: 0.1, Max: 0.2, Diff: 0.2, Sum: 0.5] 
	   [GC Worker Total (ms): Min: 2.7, Avg: 2.7, Max: 2.7, Diff: 0.1, Sum: 10.7]
	   [GC Worker End (ms): Min: 40225.4, Avg: 40225.4, Max: 40225.4, Diff: 0.1] 
	[Code Root Fixup: 0.33 ms]                                                   
	[Code Root Purge: 0.22 ms]        
	[Clear CT: 0.44 ms]          
	[Other: 1.0 ms]                                                             
	   [Choose CSet: 0.11 ms]         
	   [Ref Proc: 0.2 ms]       
	   [Ref Enq: 0.55 ms]                                                        
	   [Redirty Cards: 0.66 ms]       
	   [Humongous Register: 0.77 ms]
	   [Humongous Reclaim: 0.88 ms]                                              
	   [Free CSet: 0.99 ms]           
	[Eden: 14.0M(14.0M)->0.45B(27.0M) Survivors: 0.8B->1024.0K Heap: 14.0M(64.0M)->400.1K(64.0M)]
  [Times: user=0.01 sys=0.02, real=0.03 secs]`

	var sample []string
	for _, v := range strings.Split(logSample, "\n") {
		sample = append(sample, v)
	}

	compare := javaG1EventStatistics{
		EvacuationType:         "young",
		TotalTime:              0.0038162,
		TimeUser:               0.01,
		TimeSys:                0.02,
		TimeReal:               0.03,
		EdenUsageBefore:        14 * 1024 * 1024,
		EdenUsageAfter:         0.45,
		EdenCapacityBefore:     14 * 1024 * 1024,
		EdenCapacityAfter:      27 * 1024 * 1024,
		HeapUsageBefore:        14 * 1024 * 1024,
		HeapUsageAfter:         400.1 * 1024,
		HeapCapacityBefore:     64 * 1024 * 1024,
		HeapCapacityAfter:      64 * 1024 * 1024,
		SurvivorUsageBefore:    0.8,
		SurvivorUsageAfter:     1024 * 1024,
		ParallelProcessingTime: 2.8,
		GCWorkerCount:          4,
		GCWorkerStartDiff:      0.1,
		ExtRootScanTime:        0.7,
		CodeRootScanTime:       0.9,
		ObjectCopyTime:         1.8,
		TerminationTime:        0.1,
		TerminationAttempts:    1.5,
		GCWorkerOtherTime:      0.1,
		GCWorkerTotalTime:      2.7,
		GCWorkerEndDiff:        0.1,
		CodeRootFixupTime:      0.33,
		CodeRootPurgeTime:      0.22,
		ClearCTTime:            0.44,
		OtherTime:              1.0,
		ChooseCSetTime:         0.11,
		RefProcTime:            0.2,
		RefEnqTime:             0.55,
		RedirtyCardTime:        0.66,
		HumongousRegisterTime:  0.77,
		HumongousReclaimTime:   0.88,
		FreeCSetTime:           0.99,
	}

	if stats, err := parseJavaG1Evacuation(sample); err == nil {
		if !reflect.DeepEqual(compare, stats) {
			reflectcompare := reflect.ValueOf(compare)
			reflectstats := reflect.ValueOf(stats)
			for i, n := 0, reflectcompare.NumField(); i < n; i++ {
				field1 := reflectcompare.Field(i)
				field2 := reflectstats.Field(i)
				if field1.Interface() != field2.Interface() {
					reflectcompare := reflect.ValueOf(&compare)
					elementsCompare := reflectcompare.Elem()
					t.Fatal(fmt.Sprintf("For field %s expected %s actual %s ", elementsCompare.Type().Field(i).Name, field1, field2))
				}
			}
		}
	} else {
		t.Fatal(err)
	}
}
