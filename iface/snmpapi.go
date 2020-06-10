package iface

import (
	"github.com/soniah/gosnmp"
)

// SNMP comment.
type SNMP interface {
	BulkWalkAll(rootOid string) (results []gosnmp.SnmpPDU, err error)
	Get(oids []string) (result *gosnmp.SnmpPacket, err error)
}

// SNMPImpl comment.
type SNMPImpl struct {
	GoSNMP *gosnmp.GoSNMP
}

// BulkWalkAll comment.
func (s *SNMPImpl) BulkWalkAll(rootOid string) (results []gosnmp.SnmpPDU, err error) {
	return s.GoSNMP.BulkWalkAll(rootOid)
}

// Get comment.
func (s *SNMPImpl) Get(oids []string) (results *gosnmp.SnmpPacket, err error) {
	return s.GoSNMP.Get(oids)
}

// NewSNMP comment
func NewSNMP(s *gosnmp.GoSNMP) *SNMPImpl {
	return &SNMPImpl{
		GoSNMP: s,
	}
}
