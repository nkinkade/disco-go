package metrics

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nkinkade/disco-go/archive"
	"github.com/nkinkade/disco-go/config"
	"github.com/nkinkade/disco-go/snmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	ifAliasOid     = ".1.3.6.1.2.1.31.1.1.1.18"
	ifDescrOidStub = ".1.3.6.1.2.1.2.2.1.2"
)

// Metrics comment.
type Metrics struct {
	oids     map[string]oid
	prom     map[string]*prometheus.CounterVec
	hostname string
	machine  string
	mutex    sync.Mutex
	// TODO(kinkade): remove this field in favor of a more elegant solution.
	firstRun bool
}

type oid struct {
	name           string
	previousValue  uint64
	scope          string
	ifDescr        string
	intervalSeries archive.Model
}

func getIfaces(snmp snmp.SNMP, machine string) map[string]map[string]string {
	pdus, err := snmp.BulkWalkAll(ifAliasOid)
	if err != nil {
		log.Fatalf("Failed to walk the ifAlias OID: %v\n", err)
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
			oidMap, err := getOidsString(snmp, []string{ifDescrOid})
			if err != nil {
				log.Fatalf("Failed to determine the machine interface ifDescr: %v", err)
			}
			ifaces["machine"]["ifDescr"] = oidMap[ifDescrOid]
			ifaces["machine"]["iface"] = iface
		}
		if strings.HasPrefix(val, "uplink") {
			ifDescrOid := createOid(ifDescrOidStub, iface)
			oidMap, err := getOidsString(snmp, []string{ifDescrOid})
			if err != nil {
				log.Fatalf("Failed to determine the uplink interface ifDescr: %v", err)
			}
			ifaces["uplink"]["ifDescr"] = oidMap[ifDescrOid]
			ifaces["uplink"]["iface"] = iface
		}
	}

	return ifaces
}

func getOidsString(snmp snmp.SNMP, oids []string) (map[string]string, error) {
	oidMap := make(map[string]string)
	result, err := snmp.Get(oids)
	for _, pdu := range result.Variables {
		oidMap[pdu.Name] = string(pdu.Value.([]byte))
	}
	return oidMap, err
}

// Counter32 OIDs seem to be presented as type uint, while Counter64 OIDs seem
// to be presented as type uint64.
func getOidsInt(snmp snmp.SNMP, oids []string) (map[string]uint64,
	error) {
	oidMap := make(map[string]uint64)
	result, err := snmp.Get(oids)
	for _, pdu := range result.Variables {
		switch value := pdu.Value.(type) {
		case uint:
			oidMap[pdu.Name] = uint64(value)
		case uint64:
			oidMap[pdu.Name] = value
		default:
			log.Fatalf("Unknown type %T of SNMP type %v for OID %v\n", value, pdu.Type, pdu.Name)
		}
	}
	return oidMap, err
}

func createOid(oidStub string, iface string) string {
	return fmt.Sprintf("%v.%v", oidStub, iface)
}

// Collect comment.
func (metrics *Metrics) Collect(snmp snmp.SNMP, config config.Config) {
	// Set a lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	oids := []string{}
	for oid := range metrics.oids {
		oids = append(oids, oid)
	}
	oidValueMap, err := getOidsInt(snmp, oids)
	if err != nil {
		log.Printf("ERROR: failed to GET OIDs (%v) from SNMP server: %v", oids, err)
		// TODO(kinkade): increment some sort of error metric here.
		return
	}

	for oid, value := range oidValueMap {
		// This is less than ideal. Because we can't write to a map in a struct
		// we have to copy the whole map, modify it and then overwrite the
		// original map. There is likely a better way to do this.
		metricOid := metrics.oids[oid]
		metricOid.previousValue = value

		// If this is the first run then we have no previousValue with which to
		// calculate an increase, so we just record a previousValue and return.
		if metrics.firstRun == true {
			metrics.oids[oid] = metricOid
			continue
		}

		increase := value - metrics.oids[oid].previousValue
		ifDescr := metrics.oids[oid].ifDescr
		metricName := metrics.oids[oid].name
		metrics.prom[metricName].WithLabelValues(metrics.hostname, ifDescr).Add(float64(increase))

		metricOid.intervalSeries.Samples = append(
			metricOid.intervalSeries.Samples,
			archive.Sample{Timestamp: time.Now().Unix(), Value: increase},
		)
		metrics.oids[oid] = metricOid
	}

	if metrics.firstRun == true {
		metrics.firstRun = false
	}
}

// Write comment.
func (metrics *Metrics) Write(interval uint64) {
	// Set a lock to avoid a race between the collecting and writing of metrics.
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()

	for oid, values := range metrics.oids {
		archive.Write(values.intervalSeries, interval)
		// This is less than ideal. Because we can't write to a map in a struct
		// we have to copy the whole map, modify it and then overwrite the
		// original map. There is likely a better way to do this.
		metricsOid := metrics.oids[oid]
		metricsOid.intervalSeries.Samples = []archive.Sample{}
		metrics.oids[oid] = metricsOid
	}
}

// New implements metrics.
func New(snmp snmp.SNMP, config config.Config, target string) *Metrics {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("Failed to determine the hostname of the system: %v", err)
	}
	//machine := hostname[:5]
	machine := "mlab2"
	ifaces := getIfaces(snmp, machine)

	m := &Metrics{
		oids:     make(map[string]oid),
		prom:     make(map[string]*prometheus.CounterVec),
		hostname: hostname,
		machine:  machine,
		firstRun: true,
	}

	for _, metric := range config.Metrics {
		discoNames := map[string]string{
			"machine": metric.MlabMachineName,
			"uplink":  metric.MlabUplinkName,
		}
		for scope, values := range ifaces {
			oidStr := createOid(metric.OidStub, values["iface"])
			o := oid{
				name:    metric.Name,
				scope:   scope,
				ifDescr: values["ifDescr"],
				intervalSeries: archive.Model{
					Experiment: target,
					Hostname:   hostname,
					Metric:     discoNames[scope],
					Samples:    []archive.Sample{},
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
