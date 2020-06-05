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
	ifAliasOid     = ".1.3.6.1.2.1.31.1.1.1.18"
	ifDescrOidStub = ".1.3.6.1.2.1.2.2.1.2"
)

type sample struct {
	Timestamp int64  `json:"timestamp"`
	Value     uint64 `json:"value"`
}

type series struct {
	Experiment string   `json:"experiment"`
	Hostname   string   `json:"hostname"`
	Metric     string   `json:"metric"`
	Samples    []sample `json:"sample"`
}

// Metrics comment.
type Metrics struct {
	oids     map[string]oid
	prom     map[string]*prometheus.CounterVec
	hostname string
	machine  string
	mutex    sync.Mutex
}

type oid struct {
	name           string
	previousValue  uint64
	scope          string
	ifDescr        string
	intervalSeries series
}

func getIfaces(snmp gosnmp.GoSNMP, machine string) map[string]map[string]string {
	pdus, err := snmp.BulkWalkAll(ifAliasOid)
	if err != nil {
		log.Fatalf("Failed to walk to the ifAlias OID: %v\n", err)
	}

	ifaces := map[string]map[string]string{
		"machine": map[string]string{
			"iface":   "",
			"ifDescr": "",
		},
		"uplink": map[string]string{
			"iface":   "",
			"ifDescr": "",
		},
	}

	for _, pdu := range pdus {
		oidParts := strings.Split(pdu.Name, ".")
		iface := oidParts[len(oidParts)-1]

		b := pdu.Value.([]byte)
		val := strings.TrimSpace(string(b))
		if val == machine {
			ifDescrOid := createOid(ifDescrOidStub, iface)
			oidMap := getOidsString(&snmp, []string{ifDescrOid})
			ifaces["machine"]["ifDescr"] = oidMap[ifDescrOid]
			ifaces["machine"]["iface"] = iface
		}
		if strings.HasPrefix(val, "uplink") {
			ifDescrOid := createOid(ifDescrOidStub, iface)
			oidMap := getOidsString(&snmp, []string{ifDescrOid})
			ifaces["uplink"]["ifDescr"] = oidMap[ifDescrOid]
			ifaces["uplink"]["iface"] = iface
		}
	}

	return ifaces
}

func getOidsString(snmp *gosnmp.GoSNMP, oids []string) map[string]string {
	oidMap := make(map[string]string)
	result, _ := snmp.Get(oids)
	for _, pdu := range result.Variables {
		oidMap[pdu.Name] = string(pdu.Value.([]byte))
	}
	return oidMap
}

func getOidsUint64(snmp *gosnmp.GoSNMP, oids []string) map[string]uint64 {
	oidMap := make(map[string]uint64)
	result, _ := snmp.Get(oids)
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
	for oid, values := range metrics.oids {
		data, _ := json.MarshalIndent(values.intervalSeries, "", "    ")
		f.Write(data)
		metricsOid := metrics.oids[oid]
		metricsOid.intervalSeries = series{}
		metrics.oids[oid] = metricsOid
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
		ifDescr := metrics.oids[oid].ifDescr
		metricName := metrics.oids[oid].name
		metrics.prom[metricName].WithLabelValues(metrics.hostname, ifDescr).Add(float64(increase))
		metricOid := metrics.oids[oid]
		metricOid.previousValue = value
		metricOid.intervalSeries.Samples = append(
			metricOid.intervalSeries.Samples,
			sample{Timestamp: time.Now().Unix(), Value: increase},
		)
		metrics.oids[oid] = metricOid
	}
}

// New implements metrics.
func New(snmp gosnmp.GoSNMP, config config.Config, target string) *Metrics {
	hostname, _ := os.Hostname()
	machine := hostname[:5]
	ifaces := getIfaces(snmp, machine)

	m := &Metrics{
		oids:     make(map[string]oid),
		prom:     make(map[string]*prometheus.CounterVec),
		hostname: hostname,
		machine:  machine,
	}

	for _, metric := range config.Metrics {
		discoNames := map[string]string{
			"machine": metric.MlabMachineName,
			"uplink":  metric.MlabUplinkName,
		}
		for scope, values := range ifaces {
			oidStr := createOid(metric.OidStub, values["iface"])
			o := oid{
				name:          metric.Name,
				previousValue: 0,
				scope:         scope,
				ifDescr:       values["ifDescr"],
				intervalSeries: series{
					Experiment: target,
					Hostname:   hostname,
					Metric:     discoNames[scope],
					Samples:    []sample{},
				},
			}
			m.oids[oidStr] = o
		}
		m.prom[metric.Name] = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metric.Name,
				Help: metric.Description,
			},
			[]string{
				"node",
				"interface",
			},
		)
	}

	return m
}
