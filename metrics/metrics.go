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
	oids     map[string]*oid
	hostname string
	machine  string
	mutex    sync.Mutex
}

type oid struct {
	name           string
	previousValue  uint64
	currentValue   uint64
	scope          string
	ifDescr        string
	intervalSeries series
	prom           *prometheus.CounterVec
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

func getOidsUint64(snmp *gosnmp.GoSNMP, oids []string) map[string]uint64 {
	oidMap := make(map[string]uint64)
	result, _ := snmp.GetBulk(oids, uint8(len(oids)), 0)
	for _, pdu := range result.Variables {
		oidMap[pdu.Name] = gosnmp.ToBigInt(pdu.Value).Uint64()
	}
	return oidMap
}

func createOid(oidStub string, iface string) string {
	return fmt.Sprintf("%v.%v", oidStub, iface)
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
	fileName := fmt.Sprintf("%v-to-%v-switch.json", startTimeStr, endTimeStr)
	filePath := fmt.Sprintf("%v/%v", dirs, fileName)
	f, _ := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	for _, values := range metrics.oids {
		data, _ := json.MarshalIndent(values.intervalSeries, "", "    ")
		f.Write(data)
	}
}

// Collect comment.
func (metrics *Metrics) Collect(snmp gosnmp.GoSNMP, config config.Config) {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	oids := []string{}
	for oid := range metrics.oids {
		oids = append(oids, oid)
	}
	oidValueMap := getOidsUint64(&snmp, oids)

	for oid, value := range oidValueMap {
		increase := value - metrics.oids[oid].previousValue
		metrics.oids[oid].prom.WithLabelValues(metrics.hostname, metrics.oids[oid].ifDescr).Add(float64(increase))
		metrics.oids[oid].currentValue = increase
		series := metrics.oids[oid].intervalSeries
		series.Sample = append(series.Sample, sample{Timestamp: time.Now().Unix(), Value: increase})
		metrics.oids[oid].intervalSeries = series
	}
}

// New implements metrics.
func New(snmp gosnmp.GoSNMP, config config.Config, target string) *Metrics {
	hostname, _ := os.Hostname()
	machine := hostname[:5]
	machineIface, uplinkIface := getIfaces(snmp, machine)

	iFaces := map[string]string{
		"machine": machineIface,
		"uplink":  uplinkIface,
	}

	m := &Metrics{
		oids:     make(map[string]*oid),
		hostname: hostname,
		machine:  machine,
	}

	for _, metric := range config.Metrics {
		discoNames := map[string]string{
			"machine": metric.MlabMachineName,
			"uplink":  metric.MlabUplinkName,
		}
		for s, i := range iFaces {
			oidStr := createOid(metric.OidStub, i)
			o := &oid{
				name:          metric.Name,
				previousValue: 0,
				currentValue:  0,
				scope:         s,
				intervalSeries: series{
					Experiment: target,
					Hostname:   hostname,
					Metric:     discoNames[s],
					Sample:     []sample{},
				},
				prom: promauto.NewCounterVec(
					prometheus.CounterOpts{
						Name: metric.Name,
						Help: metric.Description,
					},
					[]string{
						"node",
						"interface",
					},
				),
			}
			m.oids[oidStr] = o
		}
	}

	return m
}
