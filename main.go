package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/elkatwork/cec"
	log "github.com/sirupsen/logrus"
)

var (
	brokers  *string
	user     *string
	pass     *string
	clientId *string
	prefix   *string

	cecClient  *cec.Connection
	mqttClient *mqtt.ConnectionManager

	err error
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
			Router:   paho.NewSingleHandlerRouter(handleMQTTMessage),
		},
	}
	mqttOpts.SetUsernamePassword(*user, []byte(*pass))

	mqttClient, err = mqtt.NewConnection(context.Background(), mqttOpts)
	if err != nil {
		log.Fatal(err)
	}

	cecClient, err = cec.Open("", "cec-mqtt", true)
	if err != nil {
		log.Fatalf("Error initializing CEC: %s", err)
	}

	go func() {
		for {
			msg := <-cecClient.MsgLog
			handleCECMessage(msg)
		}
	}()

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

func handleMQTTMessage(m *paho.Publish) {
	topic := m.Topic
	payload := string(m.Payload)
	log.Infof("received msg on topic '%s': %s", topic, payload)

	topicLevels := strings.Split(topic, "/")
	address, err := strconv.Atoi(topicLevels[len(topicLevels)-2])
	if err != nil {
		log.Errorf("Error handling MQTT message: %s", err)
		return
	}

	if payload == "on" {
		cecClient.PowerOn(address)
	} else if payload == "off" {
		cecClient.Standby(address)
	}
}

func handleCECMessage(msg string) {
	log.Debugf("CEC: %s", msg)

	re := regexp.MustCompile(`>> ([0-9a-f])[0-9a-f]:90:([0-9a-f]{2})`)
	m := re.FindAllStringSubmatch(msg, -1)
	log.Infof("cec match: %v", m)
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
