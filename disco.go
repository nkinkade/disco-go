package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
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
	"gopkg.in/yaml.v2"
)

var (
	fListenAddress          = flag.String("web.listen-address", ":8888", "Address to listen on for telemetry.")
	fMetricsFile            = flag.String("metrics", "", "Path to YAML file defining metrics to scrape.")
	ifAliasOID              = "1.3.6.1.2.1.31.1.1.1.18"
	hostname, _             = os.Hostname()
	machine                 = hostname[:5]
	target                  = os.Getenv("DISCO_TARGET")
	community               = os.Getenv("DISCO_COMMUNITY")
	mainCtx, mainCancel     = context.WithCancel(context.Background())
	logFatal                = log.Fatal
	ifDescOidStub           = "1.3.6.1.2.1.2.2.1.2.IFACE"
	mutex                   sync.Mutex
	seriesStartTime                = time.Now()
	seriesDurationSeconds   uint64 = 60
	metricUplinkPrevValues         = make(map[string]uint64)
	metricMachinePrevValues        = make(map[string]uint64)
	promMetrics                    = make(map[string]*prometheus.CounterVec)
	uplinkMetricSeries             = make(map[string]series)
	machineMetricSeries            = make(map[string]series)
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

type metricsConfig []metric

type metric struct {
	Name            string `yaml:"name"`
	Description     string `yaml:"description"`
	OidStub         string `yaml:"oidStub"`
	MlabUplinkName  string `yaml:"mlabUplinkName"`
	MlabMachineName string `yaml:"mlabMachineName"`
}

func collectMetrics(metrics metricsConfig) {
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

	for _, metric := range metrics {
		// Machine interface metrics
		mOid := strings.Replace(metric.OidStub, "IFACE", mIface, 1)
		mVal := getOidUint64(snmp, mOid)
		mIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", mIface, 1))
		mIncrease := mVal - metricMachinePrevValues[metric.Name]
		promMetrics[metric.Name].WithLabelValues(hostname, mIfDesc).Add(float64(mIncrease))
		metricMachinePrevValues[metric.Name] = mVal
		mSeries := machineMetricSeries[metric.Name]
		mSeries.Sample = append(mSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: mIncrease})
		machineMetricSeries[metric.Name] = mSeries

		// Uplink interface metrics
		uOid := strings.Replace(metric.OidStub, "IFACE", uIface, 1)
		uVal := getOidUint64(snmp, uOid)
		uIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", uIface, 1))
		uIncrease := uVal - metricUplinkPrevValues[metric.Name]
		promMetrics[metric.Name].WithLabelValues(hostname, uIfDesc).Add(float64(uIncrease))
		metricUplinkPrevValues[metric.Name] = uVal
		uSeries := uplinkMetricSeries[metric.Name]
		uSeries.Sample = append(uSeries.Sample, sample{Timestamp: time.Now().Unix(), Value: uIncrease})
		uplinkMetricSeries[metric.Name] = uSeries
	}

}

func writeMetrics() {
	// Set a global lock to avoid a race between the collecting and writing of metrics.
	mutex.Lock()
	defer mutex.Unlock()

	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/02"), hostname)
	os.MkdirAll(dirs, 0755)
	startTime := seriesStartTime.Format("2006-01-02T15:04:05")
	endTime := time.Now().Format("2006-01-02T15:04:05")
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

func initializeMaps(metrics metricsConfig) {
	for _, metric := range metrics {
		metricUplinkPrevValues[metric.Name] = 0
		metricMachinePrevValues[metric.Name] = 0

		promMetrics[metric.Name] = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metric.Name,
				Help: metric.Description,
			},
			[]string{
				"node",
				"interface",
			},
		)

		uplinkMetricSeries[metric.Name] = series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     metric.MlabUplinkName,
			Sample:     []sample{},
		}

		machineMetricSeries[metric.Name] = series{
			Experiment: target,
			Hostname:   hostname,
			Metric:     metric.MlabMachineName,
			Sample:     []sample{},
		}
	}
}

func main() {
	var metrics metricsConfig

	flag.Parse()

	yamlFile, err := ioutil.ReadFile(*fMetricsFile)
	if err != nil {
		log.Fatalf("Error reading YAML config file %v: %s\n", fMetricsFile, err)
	}

	err = yaml.Unmarshal(yamlFile, &metrics)
	if err != nil {
		log.Fatalf("Error unmarshaling YAML config: %v\n", err)
	}

	initializeMaps(metrics)

	if len(target) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_TARGET")
	}
	if len(community) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_COMMUNITY")
	}

	// Start scraping on a clean 10s boundary within a minute.
	for time.Now().Second()%10 != 0 {
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
