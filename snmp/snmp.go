package snmp

import (
	"github.com/soniah/gosnmp"
)

// SNMP comment.
type SNMP interface {
	BulkWalkAll(rootOid string) (results []gosnmp.SnmpPDU, err error)
	Get(oids []string) (result *gosnmp.SnmpPacket, err error)
}

// RealSNMP comment.
type RealSNMP struct {
	GoSNMP *gosnmp.GoSNMP
}

// BulkWalkAll comment.
func (s *RealSNMP) BulkWalkAll(rootOid string) (results []gosnmp.SnmpPDU, err error) {
	return s.GoSNMP.BulkWalkAll(rootOid)
}

// Get comment.
func (s *RealSNMP) Get(oids []string) (results *gosnmp.SnmpPacket, err error) {
	return s.GoSNMP.Get(oids)
}

// Client comment.
func Client(s *gosnmp.GoSNMP) *RealSNMP {
	return &RealSNMP{
		GoSNMP: s,
	}
}
