package main

import (
	"fmt"
	"log"
	"workloadstealworker/pkg/worker"

	"github.com/spf13/viper"
)

func main() {
	stopChan := make(chan bool)

	workerConfig := worker.Config{
		NATSURL:     getENVValue("NATS_URL"),
		NATSSubject: getENVValue("NATS_SUBJECT"),
	}
	informer, err := worker.New(workerConfig)
	if err != nil {
		log.Fatal(err)
	}

	go log.Fatal(informer.Start(stopChan))
	<-stopChan
}

func init() {
	// Initialize Viper
	viper.SetConfigType("env") // Use environment variables
	viper.AutomaticEnv()       // Automatically read environment variables
}

func getENVValue(envKey string) string {
	// Read environment variables
	value := viper.GetString(envKey)
	if value == "" {
		message := fmt.Sprintf("%s environment variable is not set", envKey)
		log.Fatal(message)
	}
	return value
}
