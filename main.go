package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chbmuc/cec"
	mqtt "github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	log "github.com/sirupsen/logrus"
)

var (
	brokers  *string
	user     *string
	pass     *string
	clientId *string
	prefix   *string

	cecClient *cec.Connection
)

func main() {
	log.Info("starting MQTT-CEC bridge...")

	brokers = flag.String("brokers", "mqtt://localhost:1883", "comma separated URL(s) for the broker (schemes supported include 'mqtt' and 'tls')")
	user = flag.String("user", "", "username to use when connecting to MQTT")
	pass = flag.String("pass", "", "password to use when connecting to MQTT")
	clientId = flag.String("clientid", "mqtt-cec-bridge", "client ID to use when connecting to MQTT")
	prefix = flag.String("prefix", "media", "the bridge will listen to the {prefix}/cec/+/cmd topic")

	flag.Parse()

	mqttOpts := mqtt.ClientConfig{
		BrokerUrls:        brokerParamToURLs(*brokers),
		ConnectRetryDelay: 1 * time.Second,
		ConnectTimeout:    1 * time.Second,
		OnConnectionUp:    subscribe,
		OnConnectError:    connectionError,
		ClientConfig: paho.ClientConfig{
			ClientID: *clientId,
			Router:   paho.NewSingleHandlerRouter(handleMsg),
		},
	}
	mqttOpts.SetUsernamePassword(*user, []byte(*pass))

	_, err := mqtt.NewConnection(context.Background(), mqttOpts)
	if err != nil {
		log.Fatal(err)
	}

	cecClient, err = cec.Open("", "cec-mqtt", true)
	if err != nil {
		log.Fatalf("Error initializing CEC: %s", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}

func subscribe(cm *mqtt.ConnectionManager, ca *paho.Connack) {
	log.Info("MQTT connection up")
	if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: map[string]paho.SubscribeOptions{
			fmt.Sprintf("%s/cec/+/cmd", *prefix): {QoS: 0},
		},
	}); err != nil {
		fmt.Printf("failed to subscribe (%s). This is likely to mean no messages will be received.", err)
		return
	}
}

func connectionError(err error) {
	log.Fatalf("MQTT connection error: %s", err)
}

func handleMsg(m *paho.Publish) {
	log.Infof("received msg on topic '%s': %s", m.Topic, m.Payload)
	payload := string(m.Payload)
	if payload == "on" {
		cecClient.PowerOn(0)
	} else if payload == "off" {
		cecClient.Standby(0)
	}
}

func brokerParamToURLs(brokers string) []*url.URL {
	urls := make([]*url.URL, 0)

	for _, broker := range strings.Split(brokers, ",") {
		url, err := url.Parse(broker)
		if err != nil {
			log.Fatal(err)
		}

		urls = append(urls, url)
	}

	return urls
}
