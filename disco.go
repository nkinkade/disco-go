package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/soniah/gosnmp"
)

var (
	fListenAddress      = flag.String("web.listen-address", ":8888", "Address to listen on for telemetry.")
	ifAliasOID          = "1.3.6.1.2.1.31.1.1.1.18"
	hostname, _         = os.Hostname()
	machine             = hostname[:5]
	target              = os.Getenv("DISCO_TARGET")
	community           = os.Getenv("DISCO_COMMUNITY")
	mainCtx, mainCancel = context.WithCancel(context.Background())
	logFatal            = log.Fatal
	ifDescOidStub       = "1.3.6.1.2.1.2.2.1.2.IFACE"
	mutex               sync.Mutex
	metricOidStubs      = map[string]string{
		"ifHCInOctets":             "1.3.6.1.2.1.31.1.1.1.6.IFACE",
		"ifHCOutOctets":            "1.3.6.1.2.1.31.1.1.1.10.IFACE",
		"ifHCInUcastPkts":          "1.3.6.1.2.1.31.1.1.1.7.IFACE",
		"ifHCOutUcastPkts":         "1.3.6.1.2.1.31.1.1.1.11.IFACE",
		"ifInErrors":               "1.3.6.1.2.1.2.2.1.14.IFACE",
		"ifOutErrors":              "1.3.6.1.2.1.2.2.1.20.IFACE",
		"jnxCosQstatTotalDropPkts": "1.3.6.1.4.1.2636.3.15.4.1.53.IFACE.0",
	}

	// Stores the last read values for each machine iface metric
	metricMachinePrevValues = map[string]uint64{
		"ifHCInOctets":             0,
		"ifHCOutOctets":            0,
		"ifHCInUcastPkts":          0,
		"ifHCOutUcastPkts":         0,
		"ifInErrors":               0,
		"ifOutErrors":              0,
		"jnxCosQstatTotalDropPkts": 0,
	}

	// Stores the last read values for each uplink iface metric
	metricUplinkPrevValues = map[string]uint64{
		"ifHCInOctets":             0,
		"ifHCOutOctets":            0,
		"ifHCInUcastPkts":          0,
		"ifHCOutUcastPkts":         0,
		"ifInErrors":               0,
		"ifOutErrors":              0,
		"jnxCosQstatTotalDropPkts": 0,
	}

	promMetrics = map[string]*prometheus.CounterVec{
		"ifHCInOctets": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCInOctets",
				Help: "Ingress octets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCOutOctets": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCOutOctets",
				Help: "Egress octets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCInUcastPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCInUcastPkts",
				Help: "Ingress unicast packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCOutUcastPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCOutUcastPkts",
				Help: "Egress unicast packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifInErrors": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifInErrors",
				Help: "Ingress errors.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifOutErrors": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifOutErrors",
				Help: "Egress errors.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"jnxCosQstatTotalDropPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jnxCosQstatTotalDropPkts",
				Help: "Dropped packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
	}

	seriesStartTime              = time.Now()
	seriesDurationSeconds uint64 = 60

	uplinkMetricSeries = map[string]series{
		"ifHCInOctets": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.octets.uplink.rx",
			Sample:     []sample{},
		},
		"ifHCOutOctets": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.octets.uplink.tx",
			Sample:     []sample{},
		},
		"ifHCInUcastPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.unicast.uplink.rx",
			Sample:     []sample{},
		},
		"ifHCOutUcastPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.unicast.uplink.tx",
			Sample:     []sample{},
		},
		"ifInErrors": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.errors.uplink.rx",
			Sample:     []sample{},
		},
		"ifOutErrors": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.errors.uplink.rx",
			Sample:     []sample{},
		},
		"jnxCosQstatTotalDropPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.discards.uplink",
			Sample:     []sample{},
		},
	}

	machineMetricSeries = map[string]series{
		"ifHCInOctets": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.octets.local.rx",
			Sample:     []sample{},
		},
		"ifHCOutOctets": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.octets.local.tx",
			Sample:     []sample{},
		},
		"ifHCInUcastPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.unicast.local.rx",
			Sample:     []sample{},
		},
		"ifHCOutUcastPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.unicast.local.tx",
			Sample:     []sample{},
		},
		"ifInErrors": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.errors.local.rx",
			Sample:     []sample{},
		},
		"ifOutErrors": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.errors.local.rx",
			Sample:     []sample{},
		},
		"jnxCosQstatTotalDropPkts": series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     "switch.discards.local",
			Sample:     []sample{},
		},
	}
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

func collectMetrics() {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	mutex.Lock()
	defer mutex.Unlock()

	snmp := &gosnmp.GoSNMP{
		Target:    target,
		Port:      uint16(161),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(2) * time.Second,
		Retries:   1,
	}

	err := snmp.Connect()
	if err != nil {
		log.Fatalf("Connect() err: %v", err)
	}
	defer snmp.Conn.Close()

	snmpPDUs, err := snmp.BulkWalkAll(ifAliasOID)
	if err != nil {
		log.Fatalf("Walk Error: %v\n", err)
	}

	mIface, uIface := getIfaces(snmpPDUs)

	for metric, oid := range metricOidStubs {
		// Machine interface metrics
		mOid := strings.Replace(oid, "IFACE", mIface, 1)
		mVal := getOidUint64(snmp, mOid)
		mIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", mIface, 1))
		mIncrease := mVal - metricMachinePrevValues[metric]
		promMetrics[metric].WithLabelValues(hostname, mIfDesc).Add(float64(mIncrease))
		metricMachinePrevValues[metric] = mVal
		mSeries := machineMetricSeries[metric]
		mSeries.Sample = append(mSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: mIncrease})
		machineMetricSeries[metric] = mSeries

		// Uplink interface metrics
		uOid := strings.Replace(oid, "IFACE", uIface, 1)
		uVal := getOidUint64(snmp, uOid)
		uIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", uIface, 1))
		uIncrease := uVal - metricUplinkPrevValues[metric]
		promMetrics[metric].WithLabelValues(hostname, uIfDesc).Add(float64(uIncrease))
		metricUplinkPrevValues[metric] = uVal
		uSeries := uplinkMetricSeries[metric]
		uSeries.Sample = append(uSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: uIncrease})
		uplinkMetricSeries[metric] = uSeries
	}

}

func writeMetrics() {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	mutex.Lock()
	defer mutex.Unlock()

	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/06"), hostname)
	os.MkdirAll(dirs, 0755)
	startTime := seriesStartTime.Format("2006-01-06T15:04:05")
	endTime := time.Now().Format("2006-01-06T15:04:05")
	filename := fmt.Sprintf("%v-to-%v-switch.json", startTime, endTime)
	filePath := fmt.Sprintf("%v/%v", dirs, filename)
	f, _ := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	for _, mValue := range machineMetricSeries {
		mData, _ := json.MarshalIndent(mValue, "", "    ")
		f.Write(mData)
	}
	for _, uValue := range uplinkMetricSeries {
		uData, _ := json.MarshalIndent(uValue, "", "    ")
		f.Write(uData)
	}
	seriesStartTime = time.Now()
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

func getIfaces(pdus []gosnmp.SnmpPDU) (string, string) {
	var machineIface string
	var uplinkIface string

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

func main() {
	flag.Parse()

	if len(target) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_TARGET")
	}
	if len(community) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_COMMUNITY")
	}

	// Start everything at the top of a minute so that series collection windows
	// are at the very least alighned to the minute.
	for time.Now().Second() != 0 {
		time.Sleep(1 * time.Second)
	}

	cronCollectMetrics := gocron.NewScheduler(time.UTC)
	cronCollectMetrics.Every(10).Seconds().StartImmediately().Do(collectMetrics)
	cronCollectMetrics.StartAsync()

	cronWriteMetrics := gocron.NewScheduler(time.UTC)
	cronWriteMetrics.Every(seriesDurationSeconds).Seconds().Do(writeMetrics)
	cronWriteMetrics.StartAsync()

	http.Handle("/metrics", promhttp.Handler())

	srv := http.Server{
		Addr:    *fListenAddress,
		Handler: http.DefaultServeMux,
	}

	fmt.Printf("Listening on port %v.\n", *fListenAddress)

	// When the context is canceled, stop serving.
	go func() {
		<-mainCtx.Done()
		srv.Close()
	}()

	// Listen forever, or until the context is closed.
	logFatal(srv.ListenAndServe())
}
