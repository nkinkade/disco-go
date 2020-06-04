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
	ifAliasOid    = "1.3.6.1.2.1.31.1.1.1.18"
	ifDescOidStub = "1.3.6.1.2.1.2.2.1.2"
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
	metricUplinkPrevValues  map[string]uint64
	metricMachinePrevValues map[string]uint64
	promMetrics             map[string]*prometheus.CounterVec
	uplinkMetricSeries      map[string]series
	machineMetricSeries     map[string]series
	mutex                   sync.Mutex
	hostname                string
	machine                 string
	machineIface            string
	uplinkIface             string
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

func putMetrics() {

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

// Collect comment.
func (metrics *Metrics) Collect(snmp gosnmp.GoSNMP, config config.Config) {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	for _, metric := range config.Metrics {
		// Machine interface metrics
		mOid := createOid(metric.OidStub, metrics.machineIface)
		mVal := getOidUint64(&snmp, mOid)
		mIfDesc := getOidString(&snmp, createOid(ifDescOidStub, metrics.machineIface))
		mIncrease := mVal - metrics.metricMachinePrevValues[metric.Name]
		metrics.promMetrics[metric.Name].WithLabelValues(metrics.hostname, mIfDesc).Add(float64(mIncrease))
		metrics.metricMachinePrevValues[metric.Name] = mVal
		mSeries := metrics.machineMetricSeries[metric.Name]
		mSeries.Sample = append(mSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: mIncrease})
		metrics.machineMetricSeries[metric.Name] = mSeries

		// Uplink interface metrics
		uOid := createOid(metric.OidStub, metrics.uplinkIface)
		uVal := getOidUint64(&snmp, uOid)
		uIfDesc := getOidString(&snmp, createOid(ifDescOidStub, metrics.uplinkIface))
		uIncrease := uVal - metrics.metricUplinkPrevValues[metric.Name]
		metrics.promMetrics[metric.Name].WithLabelValues(metrics.hostname, uIfDesc).Add(float64(uIncrease))
		metrics.metricUplinkPrevValues[metric.Name] = uVal
		uSeries := metrics.uplinkMetricSeries[metric.Name]
		uSeries.Sample = append(uSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: uIncrease})
		metrics.uplinkMetricSeries[metric.Name] = uSeries
	}
}

// Write comment.
func (metrics *Metrics) Write(interval uint64) {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/02"), metrics.hostname)
	os.MkdirAll(dirs, 0755)
	startTime := time.Now().Add(int(time.Duration(interval) * -time.Second)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")
	endTimeStr := time.Now().Format("2006-01-02T15:04:05")
	filename := fmt.Sprintf("%v-to-%v-switch.json", startTimeStr, endTimeStr)
	filePath := fmt.Sprintf("%v/%v", dirs, filename)
	f, _ := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	for _, mValue := range metrics.machineMetricSeries {
		mData, _ := json.MarshalIndent(mValue, "", "    ")
		f.Write(mData)
	}
	for _, uValue := range metrics.uplinkMetricSeries {
		uData, _ := json.MarshalIndent(uValue, "", "    ")
		f.Write(uData)
	}
}

// New implements metrics.
func New(snmp gosnmp.GoSNMP, config config.Config, target string) Metrics {
	hostname, _ := os.Hostname()
	machine := hostname[:5]

	machineIface, uplinkIface := getIfaces(snmp, machine)

	metrics := Metrics{
		metricUplinkPrevValues:  make(map[string]uint64),
		metricMachinePrevValues: make(map[string]uint64),
		promMetrics:             make(map[string]*prometheus.CounterVec),
		uplinkMetricSeries:      make(map[string]series),
		machineMetricSeries:     make(map[string]series),
		hostname:                hostname,
		machine:                 machine,
		machineIface:            machineIface,
		uplinkIface:             uplinkIface,
	}

	for _, metric := range config.Metrics {
		metrics.metricUplinkPrevValues[metric.Name] = 0
		metrics.metricMachinePrevValues[metric.Name] = 0

		metrics.promMetrics[metric.Name] = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metric.Name,
				Help: metric.Description,
			},
			[]string{
				"node",
				"interface",
			},
		)

		metrics.uplinkMetricSeries[metric.Name] = series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     metric.MlabUplinkName,
			Sample:     []sample{},
		}

		metrics.machineMetricSeries[metric.Name] = series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     metric.MlabMachineName,
			Sample:     []sample{},
		}
	}

	return metrics
}
