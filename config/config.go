package config

import (
	"io/ioutil"

	"github.com/m-lab/go/rtx"
	"gopkg.in/yaml.v2"
)

// Config comment.
type Config struct {
	Metrics []Metric
}

// Metric comment.
type Metric struct {
	Name            string `yaml:"name"`
	Description     string `yaml:"description"`
	OidStub         string `yaml:"oidStub"`
	MlabUplinkName  string `yaml:"mlabUplinkName"`
	MlabMachineName string `yaml:"mlabMachineName"`
}

// New returns a new config struct.
func New(yamlFile string) Config {
	var c Config

	yamlData, err := ioutil.ReadFile(yamlFile)
	rtx.Must(err, "Error reading YAML metrics config file")

	err = yaml.Unmarshal(yamlData, &c.Metrics)
	rtx.Must(err, "Error unmarshaling YAML metrics config")

	return c
}
