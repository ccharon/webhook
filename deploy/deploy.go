package deploy

import (
	"log"
	"os"
	"os/exec"
	"sync/atomic"
	"webhook/config"
)

// Deployment allow only one at a time. Requests that
// arrive while a deployment is running are discarded.
type Deployment struct {
	configuration *config.Configuration
	deployRunning atomic.Bool
}

func NewDeployment(configuration *config.Configuration) *Deployment {
	return &Deployment{configuration: configuration}
}

func (d *Deployment) Execute(id string, image string) (started bool) {
	if !d.deployRunning.Load() {
		d.deployRunning.Store(true)
		go d.runScript(id, image)
		return true
	}
	return false
}

func (d *Deployment) runScript(id string, image string) {
	log.Printf("running script %s", d.configuration.Script())

	cmd := exec.Command(d.configuration.Script())
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		"DEPLOY_ID="+id,
		"DEPLOY_IMAGE="+image,
	)
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		log.Println("command execution failed: ", err)
	}

	d.deployRunning.Store(false)
}
