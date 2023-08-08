package main

import (
	"log"
	"os"
	"os/exec"
	"sync/atomic"
)

// Allow only one deployment at a time. Deployment requests that
// arrive while a deployment is running are discarded.
var deployRunning = atomic.Bool{}

func execDeployment(hook Hook, r *atomic.Bool) {
	log.Printf("running script %s", configuration.Script)

	cmd := exec.Command(configuration.Script)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env,
		"DEPLOY_ID="+hook.Id,
		"DEPLOY_IMAGE="+hook.Image,
	)
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		log.Println("command execution failed: ", err)
	}

	r.Store(false)
}
