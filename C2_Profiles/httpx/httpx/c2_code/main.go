package main

import (
	"github.com/MythicMeta/MythicContainer/logging"
	"mythicHTTP/webserver"
	"os"
)

func main() {
	err := webserver.InitializeLocalConfig()
	if err != nil {
		os.Exit(1)
	}
	err = webserver.InitializeLocalAgentConfig()
	if err != nil {
		os.Exit(1)
	}
	for index, instance := range webserver.Config.Instances {
		logging.LogInfo("Initializing webserver", "instance", index+1)
		router := webserver.Initialize(instance)
		// start serving up API routes
		logging.LogInfo("Starting webserver", "instance", index+1)
		webserver.StartServer(router, instance)
	}

	forever := make(chan bool)
	<-forever

}
