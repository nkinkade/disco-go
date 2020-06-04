package config

import (
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Metrics []metric
}

type metric struct {
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
	if err != nil {
		log.Fatalf("Error reading YAML config file %v: %s\n", yamlFile, err)
	}

	err = yaml.Unmarshal(yamlData, &c.Metrics)
	if err != nil {
		log.Fatalf("Error unmarshaling YAML config: %v\n", err)
	}

	return c
}
