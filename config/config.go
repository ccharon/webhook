package config

import (
	"log"
	"os"
	"webhook/util"
)

// Configuration reads config values from a file, it is an example of how to make properties available as read only.
// This works for everything that is not in this package. Access is only possible via Exported Functions
type Configuration struct {
	address string
	port    int
	token   string
	script  string
}

type configdata struct {
	Server server `json:"server"`
	Token  string `json:"token"`
	Script string `json:"script"`
}

type server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func NewConfiguration(configLocation string) (configuration *Configuration, err error) {
	// read configuration file from disk
	configFile, err := os.Open(configLocation)
	if err != nil {
		return nil, err
	}

	defer func(configFile *os.File) {
		err := configFile.Close()
		if err != nil {
			log.Println("Configfile could not be closed")
		}
	}(configFile)

	// unmarshal json to config object
	cfg, err := util.Unmarshal[configdata](configFile)
	if err != nil {
		return nil, err
	}
	c := Configuration{
		address: cfg.Server.Host,
		port:    cfg.Server.Port,
		token:   cfg.Token,
		script:  cfg.Script,
	}

	return &c, nil
}

func (c *Configuration) Address() (address string) {
	return c.address
}
func (c *Configuration) Port() (port int) {
	return c.port
}
func (c *Configuration) Token() (token string) {
	return c.token
}
func (c *Configuration) Script() (script string) {
	return c.script
}
