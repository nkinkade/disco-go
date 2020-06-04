package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nkinkade/disco-go/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/soniah/gosnmp"
)

const (
	ifAliasOid     = "1.3.6.1.2.1.31.1.1.1.18"
	ifDescrOidStub = "1.3.6.1.2.1.2.2.1.2"
)

type sample struct {
	Timestamp int64  `json:"timestamp"`
	Value     uint64 `json:"value"`
}

type series struct {
	Experiment string   `json:"experiment"`
	Hostname   string   `json:"hostname"`
	Metric     string   `json:"metric"`
	Sample     []sample `json:"sample"`
}

// Metrics comment.
type Metrics struct {
	metricsPrevValues map[string]map[string]uint64
	metricsSeries     map[string]map[string]series
	metricsPrometheus map[string]*prometheus.CounterVec
	mutex             sync.Mutex
	hostname          string
	machine           string
	machineIface      string
	uplinkIface       string
}

func getIfaces(s gosnmp.GoSNMP, machine string) (string, string) {
	var machineIface string
	var uplinkIface string

	pdus, err := s.BulkWalkAll(ifAliasOid)
	if err != nil {
		log.Fatalf("Failed to walk to the ifAlias OID: %v\n", err)
	}

	for _, pdu := range pdus {
		oidParts := strings.Split(pdu.Name, ".")
		iface := oidParts[len(oidParts)-1]

		b := pdu.Value.([]byte)
		val := strings.TrimSpace(string(b))
		if val == machine {
			machineIface = iface
		}
		if strings.HasPrefix(val, "uplink") {
			uplinkIface = iface
		}
	}
	return machineIface, uplinkIface
}

func getOidString(snmp *gosnmp.GoSNMP, oid string) string {
	result, _ := snmp.Get([]string{oid})
	pdu := result.Variables[0]
	b := pdu.Value.([]byte)
	return string(b)
}

func getOidUint64(snmp *gosnmp.GoSNMP, oid string) uint64 {
	result, _ := snmp.Get([]string{oid})
	pdu := result.Variables[0]
	return gosnmp.ToBigInt(pdu.Value).Uint64()
}

func createOid(oidStub string, iface string) string {
	return fmt.Sprintf("%v.%v", oidStub, iface)
}

func (metrics *Metrics) putMetrics(snmp gosnmp.GoSNMP, metric config.Metric, iface string, ifaceType string) {
	oid := createOid(metric.OidStub, iface)
	val := getOidUint64(&snmp, oid)
	ifDescr := getOidString(&snmp, createOid(ifDescrOidStub, iface))
	increase := val - metrics.metricsPrevValues[metric.Name][ifaceType]
	metrics.metricsPrometheus[metric.Name].WithLabelValues(metrics.hostname, ifDescr).Add(float64(increase))
	metrics.metricsPrevValues[metric.Name][ifaceType] = val
	series := metrics.metricsSeries[metric.Name][ifaceType]
	series.Sample = append(series.Sample, sample{Timestamp: time.Now().Unix(), Value: increase})
	metrics.metricsSeries[metric.Name][ifaceType] = series
}

// Collect comment.
func (metrics *Metrics) Collect(snmp gosnmp.GoSNMP, config config.Config) {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	for _, metric := range config.Metrics {
		metrics.putMetrics(snmp, metric, metrics.machineIface, "machine")
		metrics.putMetrics(snmp, metric, metrics.uplinkIface, "uplink")
	}
}

// Write comment.
func (metrics *Metrics) Write(interval uint64) {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/02"), metrics.hostname)
	os.MkdirAll(dirs, 0755)
	startTime := time.Now().Add(time.Duration(interval) * -time.Second)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")
	endTimeStr := time.Now().Format("2006-01-02T15:04:05")
	filename := fmt.Sprintf("%v-to-%v-switch.json", startTimeStr, endTimeStr)
	filePath := fmt.Sprintf("%v/%v", dirs, filename)
	f, _ := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	for _, metric := range metrics.metricsSeries {
		mData, _ := json.MarshalIndent(metric["machine"], "", "    ")
		f.Write(mData)
		uData, _ := json.MarshalIndent(metric["uplink"], "", "    ")
		f.Write(uData)
	}
}

// New implements metrics.
func New(snmp gosnmp.GoSNMP, config config.Config, target string) *Metrics {
	hostname, _ := os.Hostname()
	machine := hostname[:5]

	machineIface, uplinkIface := getIfaces(snmp, machine)

	metrics := &Metrics{
		metricsPrevValues: make(map[string]map[string]uint64),
		metricsSeries:     make(map[string]map[string]series),
		metricsPrometheus: make(map[string]*prometheus.CounterVec),
		hostname:          hostname,
		machine:           machine,
		machineIface:      machineIface,
		uplinkIface:       uplinkIface,
	}

	for _, metric := range config.Metrics {
		metrics.metricsPrevValues[metric.Name] = map[string]uint64{
			"machine": 0,
			"uplink":  0,
		}

		metrics.metricsPrometheus[metric.Name] = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metric.Name,
				Help: metric.Description,
			},
			[]string{
				"node",
				"interface",
			},
		)

		metrics.metricsSeries[metric.Name] = map[string]series{
			"machine": series{
				Experiment: target,
				Hostname:   hostname,
				Metric:     metric.MlabMachineName,
				Sample:     []sample{},
			},
			"uplink": series{
				Experiment: target,
				Hostname:   hostname,
				Metric:     metric.MlabUplinkName,
				Sample:     []sample{},
			},
		}
	}

	return metrics
}
