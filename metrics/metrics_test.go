package metrics

import (
	"reflect"
	"testing"

	"github.com/nkinkade/disco-go/archive"
	"github.com/nkinkade/disco-go/config"
	"github.com/soniah/gosnmp"
)

var target = "s1-abc0t.measurement-lab.org"
var hostname = "mlab2-abc0t.mlab-sandbox.measurement-lab.org"
var machine = "mlab2"

var c = config.Config{
	Metrics: []config.Metric{
		config.Metric{
			Name:            "ifHCInOctets",
			Description:     "Ingress octets.",
			OidStub:         ".1.3.6.1.2.1.31.1.1.1.6",
			MlabUplinkName:  "switch.octets.uplink.rx",
			MlabMachineName: "switch.octets.local.rx",
		},
		config.Metric{
			Name:            "ifOutDiscards",
			Description:     "Egress discards.",
			OidStub:         ".1.3.6.1.2.1.2.2.1.19",
			MlabUplinkName:  "switch.discards.uplink.tx",
			MlabMachineName: "switch.discards.local.tx",
		},
	},
}

var snmpPacketMachine = gosnmp.SnmpPacket{
	Variables: []gosnmp.SnmpPDU{
		{
			Name:  ".1.3.6.1.2.1.2.2.1.2.524",
			Type:  gosnmp.OctetString,
			Value: []byte("xe-0/0/12"),
		},
	},
}

var snmpPacketUplink = gosnmp.SnmpPacket{
	Variables: []gosnmp.SnmpPDU{
		{
			Name:  ".1.3.6.1.2.1.2.2.1.2.568",
			Type:  gosnmp.OctetString,
			Value: []byte("xe-0/0/45"),
		},
	},
}

var snmpPacketMetrics = gosnmp.SnmpPacket{
	Variables: []gosnmp.SnmpPDU{
		{
			// ifOutDicards machine
			Name:  ".1.3.6.1.2.1.2.2.1.19.524.",
			Type:  gosnmp.Counter32,
			Value: uint(0),
		},
		{
			// ifOutDiscards uplink
			Name:  ".1.3.6.1.2.1.2.2.1.19.568.",
			Type:  gosnmp.Counter32,
			Value: uint(3),
		},
		{
			// ifHCInOctets machine
			Name:  ".1.3.6.1.2.1.31.1.1.1.6.524",
			Type:  gosnmp.Counter64,
			Value: uint64(275),
		},
		{
			// ifHCInOctets uplink
			Name:  ".1.3.6.1.2.1.31.1.1.1.6.568",
			Type:  gosnmp.Counter64,
			Value: uint64(437),
		},
	},
}

var expectedMetricsOIDs = map[string]oid{
	".1.3.6.1.2.1.2.2.1.19.524": oid{
		name:          "ifOutDiscards",
		previousValue: 0,
		scope:         "machine",
		ifDescr:       "xe-0/0/12",
		intervalSeries: archive.Model{
			Experiment: "s1-abc0t.measurement-lab.org",
			Hostname:   "mlab2-abc0t.mlab-sandbox.measurement-lab.org",
			Metric:     "switch.discards.local.tx",
			Samples:    []archive.Sample{},
		},
	},
	".1.3.6.1.2.1.2.2.1.19.568": oid{
		name:          "ifOutDiscards",
		previousValue: 0,
		scope:         "uplink",
		ifDescr:       "xe-0/0/45",
		intervalSeries: archive.Model{
			Experiment: "s1-abc0t.measurement-lab.org",
			Hostname:   "mlab2-abc0t.mlab-sandbox.measurement-lab.org",
			Metric:     "switch.discards.uplink.tx",
			Samples:    []archive.Sample{},
		},
	},
	".1.3.6.1.2.1.31.1.1.1.6.524": oid{
		name:          "ifHCInOctets",
		previousValue: 0,
		scope:         "machine",
		ifDescr:       "xe-0/0/12",
		intervalSeries: archive.Model{
			Experiment: "s1-abc0t.measurement-lab.org",
			Hostname:   "mlab2-abc0t.mlab-sandbox.measurement-lab.org",
			Metric:     "switch.octets.local.rx",
			Samples:    []archive.Sample{},
		},
	},
	".1.3.6.1.2.1.31.1.1.1.6.568": oid{
		name:          "ifHCInOctets",
		previousValue: 0,
		scope:         "uplink",
		ifDescr:       "xe-0/0/45",
		intervalSeries: archive.Model{
			Experiment: "s1-abc0t.measurement-lab.org",
			Hostname:   "mlab2-abc0t.mlab-sandbox.measurement-lab.org",
			Metric:     "switch.octets.uplink.rx",
			Samples:    []archive.Sample{},
		},
	},
}

type mockRealSNMP struct {
	err error
}

func (m *mockRealSNMP) BulkWalkAll(rootOid string) (results []gosnmp.SnmpPDU, err error) {
	return []gosnmp.SnmpPDU{
		{
			Name:  ".1.3.6.1.2.1.31.1.1.1.18.524",
			Type:  gosnmp.OctetString,
			Value: []byte("mlab2"),
		},
		{
			Name:  ".1.3.6.1.2.1.31.1.1.1.18.568",
			Type:  gosnmp.OctetString,
			Value: []byte("uplink-10g"),
		},
	}, nil
}

func (m *mockRealSNMP) Get(oids []string) (result *gosnmp.SnmpPacket, err error) {
	var packet *gosnmp.SnmpPacket

	// len(oids) will only be one when looking up ifDescr.
	if len(oids) == 1 {
		if oids[0] == ".1.3.6.1.2.1.2.2.1.2.524" {
			packet = &snmpPacketMachine
		}
		if oids[0] == ".1.3.6.1.2.1.2.2.1.2.568" {
			packet = &snmpPacketUplink
		}
	}

	// len(oids) will be greater than one when looking up metrics.
	if len(oids) > 1 {
		packet = &snmpPacketMetrics
	}

	return packet, m.err
}

func Test_New(t *testing.T) {
	s := &mockRealSNMP{
		err: nil,
	}
	m := New(s, c, target, hostname)

	if !reflect.DeepEqual(m.oids, expectedMetricsOIDs) {
		t.Errorf("Unexpected Metrics.oids.\nGot:\n%v\nExpected:\n%v", m.oids, expectedMetricsOIDs)
	}

	if m.hostname != hostname {
		t.Errorf("Unexpected Metrics.hostname.\nGot: %v\nExpected: %v", m.hostname, hostname)
	}

	if m.machine != machine {
		t.Errorf("Unexpected Metrics.machine.\nGot: %v\nExpected: %v", m.machine, machine)
	}

	if !m.firstRun {
		t.Errorf("Metrics.firstRun should be true, but got false.")
	}

}

func Test_Collect(t *testing.T) {
	s := &mockRealSNMP{
		err: nil,
	}
	m := New(s, c, target, hostname)

	m.Collect(s, c)

	if m.oids[".1.3.6.1.2.1.2.2.1.19.524"].previousValue != 0 {
		t.Errorf("Expected previousValue of 0 for ifOutDiscards  for machine but got: %v", m.oids[".1.3.6.1.2.1.2.2.1.19.524."].previousValue)
	}
	if m.oids[".1.3.6.1.2.1.2.2.1.19.568"].previousValue != 3 {
		t.Errorf("Expected previousValue of 3 for ifOutDiscards for uplink but got: %v", m.oids[".1.3.6.1.2.1.2.2.1.19.568."].previousValue)
	}
	if m.oids[".1.3.6.1.2.1.31.1.1.1.6.524"].previousValue != 275 {
		t.Errorf("Expected previousValue of 275 for ifHCInOctets for machine but got: %v", m.oids[".1.3.6.1.2.1.31.1.1.1.6.524"].previousValue)
	}
	if m.oids[".1.3.6.1.2.1.31.1.1.1.6.568"].previousValue != 437 {
		t.Errorf("Expected previousValue of 437 for ifHCInOctets for uplink but got: %v", m.oids[".1.3.6.1.2.1.31.1.1.1.6.568"].previousValue)
	}

}
